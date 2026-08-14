package config

// 构建期注入的版本信息（scripts/build-macos-app.sh 经 -ldflags -X 写入）。
// 放 config 包是因为 httpapi 与 service 都要读，而两者不许互相依赖。

// Version 是当前构建的版本号，随 /api/health 与更新检查返回。
var Version = "0.1.0"

// DefaultUpdateRepo 是发布版本的 GitHub 公开仓库（owner/repo），
// 更新检查读它的 Releases；可被 ACP_UPDATE_REPO 环境变量覆盖。
var DefaultUpdateRepo = "HuLuca1998/acpp"
