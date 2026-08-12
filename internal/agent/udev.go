package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type UdevMode string

const (
	UdevAuto  UdevMode = "auto"
	UdevForce UdevMode = "force"
	UdevOff   UdevMode = "off"
)

func ResolveUdevShim(mode string, uidMap []byte) (bool, error) {
	switch UdevMode(strings.ToLower(strings.TrimSpace(mode))) {
	case "", UdevAuto:
		fields := strings.Fields(strings.SplitN(string(uidMap), "\n", 2)[0])
		if len(fields) < 3 {
			return false, nil
		}
		inside, insideErr := strconv.ParseUint(fields[0], 10, 64)
		outside, outsideErr := strconv.ParseUint(fields[1], 10, 64)
		if insideErr != nil || outsideErr != nil {
			return false, nil
		}
		return inside == 0 && outside != 0, nil
	case UdevForce:
		return true, nil
	case UdevOff:
		return false, nil
	default:
		return false, errors.New("USE_UDEV_SHIM must be one of: auto, force, off")
	}
}

func UdevShimEnabledFromEnvironment() (bool, error) {
	uidMap, err := os.ReadFile("/proc/self/uid_map")
	if err != nil {
		return false, fmt.Errorf("read user namespace mapping: %w", err)
	}
	return ResolveUdevShim(os.Getenv("USE_UDEV_SHIM"), uidMap)
}

type dockerAPI struct {
	client *http.Client
}

func newDockerAPI(socket string) *dockerAPI {
	return &dockerAPI{client: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}}}
}

func (api *dockerAPI) request(ctx context.Context, method, target string) ([]byte, int, error) {
	request, err := http.NewRequestWithContext(ctx, method, "http://docker"+target, nil)
	if err != nil {
		return nil, 0, err
	}
	response, err := api.client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	return body, response.StatusCode, err
}

type supervisorInspect struct {
	Config struct {
		Env []string `json:"Env"`
	} `json:"Config"`
	Mounts []struct {
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
}

func MigrateSupervisorUdev(ctx context.Context, socket string, enabled bool) (bool, error) {
	if !enabled {
		return false, nil
	}
	api := newDockerAPI(socket)
	body, status, err := api.request(ctx, http.MethodGet, "/containers/hassio_supervisor/json")
	if err != nil {
		return false, fmt.Errorf("inspect hassio_supervisor: %w", err)
	}
	if status == http.StatusNotFound {
		return false, nil
	}
	if status < 200 || status >= 300 {
		return false, fmt.Errorf("inspect hassio_supervisor: Docker API status %d: %s", status, strings.TrimSpace(string(body)))
	}
	var inspect supervisorInspect
	if err := json.Unmarshal(body, &inspect); err != nil {
		return false, fmt.Errorf("decode hassio_supervisor inspection: %w", err)
	}
	active, pythonPath, bind := false, false, false
	for _, value := range inspect.Config.Env {
		if value == "USE_UDEV_SHIM=active" {
			active = true
		}
		if strings.HasPrefix(value, "PYTHONPATH=") && containsString(strings.Split(strings.TrimPrefix(value, "PYTHONPATH="), ":"), udevShimPath) {
			pythonPath = true
		}
	}
	for _, mount := range inspect.Mounts {
		if mount.Source == "/opt/haos-one-agent/udev-shim" && mount.Destination == udevShimPath && !mount.RW {
			bind = true
		}
	}
	if active && pythonPath && bind {
		return false, nil
	}
	body, status, err = api.request(ctx, http.MethodDelete, "/containers/hassio_supervisor?force=1")
	if err != nil {
		return false, fmt.Errorf("recreate hassio_supervisor: %w", err)
	}
	if status < 200 || status >= 300 {
		return false, fmt.Errorf("remove hassio_supervisor: Docker API status %d: %s", status, strings.TrimSpace(string(body)))
	}
	return true, nil
}
