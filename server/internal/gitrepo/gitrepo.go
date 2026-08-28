// Package gitrepo 提供 git 仓库地址的校验与命名推导。
// project（工作区克隆）与 discord（频道工作区克隆）共用，叶子包，不依赖本项目其他包。
package gitrepo

import (
	"regexp"
	"strings"
)

// cloneURLRe 只放行 https 与 scp 形式的 git URL。挡掉 `file://`、`ext::`
// 这类能在本机乱指或直接执行命令的传输方式。
var cloneURLRe = regexp.MustCompile(`^(https://[\w.-]+/[\w./~-]+|[\w.-]+@[\w.-]+:[\w./~-]+)$`)

// ValidCloneURL 报告一个地址是否是可安全交给 git clone 的仓库 URL。
func ValidCloneURL(url string) bool {
	return cloneURLRe.MatchString(url)
}

// Name 把仓库 URL 还原成 `<组织>/<仓库>`——各处克隆落点都用这两层命名。
func Name(url string) string {
	trimmed := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(url), "/"), ".git")
	if _, after, ok := strings.Cut(trimmed, "@"); ok {
		// git@github.com:owner/repo
		if _, path, ok := strings.Cut(after, ":"); ok {
			trimmed = path
		}
	}
	segments := strings.Split(trimmed, "/")
	if len(segments) >= 2 {
		return segments[len(segments)-2] + "/" + segments[len(segments)-1]
	}
	return trimmed
}
