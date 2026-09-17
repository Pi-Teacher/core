// Package version 暴露服务端的版本信息.
package version

// Version 是 /api/cli/system/info 返回的语义化版本号.
// 发布构建时通过链接参数覆盖, 源码里的值仅作开发期兜底:
//
//	go build -ldflags "-X github.com/Pi-Teacher/server/internal/platform/version.Version=1.2.3"
var Version = "1.0.0"
