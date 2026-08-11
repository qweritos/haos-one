//go:build tools

// Package tooldeps records source-built release helpers that are not imported
// by haos-one-net itself. This keeps all platform checksums in go.sum.
package tooldeps

import _ "golang.zx2c4.com/wireguard/tun"
