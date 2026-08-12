package agent

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

const (
	compatWarning = "HAOS compat: intercepted"
	udevShimBind  = "/opt/haos-one-agent/udev-shim:/opt/haos-udev-shim:ro"
	udevShimPath  = "/opt/haos-udev-shim"
)

var (
	responseTargetRE = regexp.MustCompile(`^/(?:v[^/]+/)?(info|containers/json)$`)
	createTargetRE   = regexp.MustCompile(`^/(?:v[^/]+/)?containers/create$`)
)

func normalizeDockerTarget(target string) string {
	parsed, err := url.ParseRequestURI(target)
	if err != nil {
		return ""
	}
	match := responseTargetRE.FindStringSubmatch(parsed.Path)
	if match == nil {
		return ""
	}
	return "/" + match[1]
}

func isContainerCreate(target string) bool {
	parsed, err := url.ParseRequestURI(target)
	return err == nil && createTargetRE.MatchString(parsed.Path)
}

func rewriteCreatePayload(target string, payload []byte, injectUdev bool) ([]byte, bool) {
	if !isContainerCreate(target) {
		return payload, false
	}
	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		return payload, false
	}
	changed := false
	if _, exists := data["Domainname"]; exists {
		delete(data, "Domainname")
		changed = true
	}
	hostConfig, _ := data["HostConfig"].(map[string]any)
	if hostConfig != nil {
		if _, exists := hostConfig["Ulimits"]; exists {
			delete(hostConfig, "Ulimits")
			changed = true
		}
	}
	if injectUdev && isSupervisorCreate(target, data) {
		if injectSupervisorUdev(data) {
			changed = true
		}
	}
	if !changed {
		return payload, false
	}
	rewritten, err := json.Marshal(data)
	if err != nil {
		return payload, false
	}
	return rewritten, true
}

func isSupervisorCreate(target string, data map[string]any) bool {
	parsed, _ := url.ParseRequestURI(target)
	for _, name := range parsed.Query()["name"] {
		if strings.TrimPrefix(name, "/") == "hassio_supervisor" {
			return true
		}
	}
	image, _ := data["Image"].(string)
	return strings.Contains(strings.ToLower(image), "hassio-supervisor")
}

func injectSupervisorUdev(data map[string]any) bool {
	changed := false
	hostConfig, ok := data["HostConfig"].(map[string]any)
	if !ok {
		hostConfig = map[string]any{}
		data["HostConfig"] = hostConfig
		changed = true
	}
	binds, ok := stringSlice(hostConfig["Binds"])
	if !ok {
		binds = nil
	}
	if !containsBindDestination(binds, udevShimPath) {
		binds = append(binds, udevShimBind)
		hostConfig["Binds"] = binds
		changed = true
	}

	environment, ok := stringSlice(data["Env"])
	if !ok {
		environment = nil
	}
	pythonIndex, shimIndex := -1, -1
	for index, value := range environment {
		if strings.HasPrefix(value, "PYTHONPATH=") {
			pythonIndex = index
		}
		if strings.HasPrefix(value, "USE_UDEV_SHIM=") {
			shimIndex = index
		}
	}
	if pythonIndex < 0 {
		environment = append(environment, "PYTHONPATH="+udevShimPath)
		changed = true
	} else {
		current := strings.TrimPrefix(environment[pythonIndex], "PYTHONPATH=")
		paths := strings.Split(current, ":")
		if !containsString(paths, udevShimPath) {
			environment[pythonIndex] = "PYTHONPATH=" + strings.TrimSuffix(udevShimPath+":"+current, ":")
			changed = true
		}
	}
	if shimIndex < 0 {
		environment = append(environment, "USE_UDEV_SHIM=active")
		changed = true
	} else if environment[shimIndex] != "USE_UDEV_SHIM=active" {
		environment[shimIndex] = "USE_UDEV_SHIM=active"
		changed = true
	}
	data["Env"] = environment
	return changed
}

func rewriteInfoPayload(payload []byte) ([]byte, bool) {
	var data map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&data); err != nil {
		return payload, false
	}
	warnings, _ := stringSlice(data["Warnings"])
	if containsString(warnings, compatWarning) {
		return payload, false
	}
	warnings = append(warnings, compatWarning)
	data["Warnings"] = warnings
	rewritten, err := json.Marshal(data)
	if err != nil {
		return payload, false
	}
	return rewritten, true
}

func stringSlice(value any) ([]string, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case []string:
		return append([]string(nil), typed...), true
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			result = append(result, text)
		}
		return result, true
	default:
		return nil, false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsBindDestination(binds []string, destination string) bool {
	for _, bind := range binds {
		parts := strings.SplitN(bind, ":", 3)
		if len(parts) > 1 && parts[1] == destination {
			return true
		}
	}
	return false
}
