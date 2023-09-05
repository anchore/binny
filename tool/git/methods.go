package git

import "strings"

const ResolveMethod = "git"

func IsResolveMethod(method string) bool {
	return strings.ToLower(method) == ResolveMethod
}
