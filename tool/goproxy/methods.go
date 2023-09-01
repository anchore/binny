package goproxy

import "strings"

const ResolveMethod = "goproxy"

func IsResolveMethod(method string) bool {
	switch strings.ToLower(method) {
	case "go-proxy", "go proxy", ResolveMethod:
		return true
	}
	return false
}
