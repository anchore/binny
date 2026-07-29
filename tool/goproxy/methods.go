package goproxy

import "strings"

const ResolveMethod = "go-proxy"

func IsResolveMethod(method string) bool {
	switch strings.ToLower(method) {
	case "goproxy", "go proxy", ResolveMethod:
		return true
	}
	return false
}
