package remote

import (
	"context"
	"strings"
	"testing"
)

// 契约：工具集是固定的七个只读工具。数量与名字变了要有人知道——
// 它们是 claude 侧预批清单（mount.go allowedTools）的另一半，
// 两边对不上就会出现「模型看得见但每次都弹权限卡」。
func TestService_Tools_MatchAllowedList(t *testing.T) {
	svc, _ := testService(t)
	tools := svc.tools(Scope{})

	got := map[string]bool{}
	for _, tool := range tools {
		got[tool.Name] = true
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s 必须标只读——这一面全部只读，标错会让 claude 侧弹权限卡", tool.Name)
		}
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("%s 没有描述：挂载时不注入提示词，描述是模型判断何时用它的唯一依据", tool.Name)
		}
	}

	for _, full := range allowedTools() {
		name := strings.TrimPrefix(full, "mcp__"+mcpServerName+"__")
		if !got[name] {
			t.Errorf("预批清单里的 %s 不在工具集里", name)
		}
	}
	if len(tools) != len(allowedTools()) {
		t.Errorf("工具数 %d 与预批清单 %d 对不上", len(tools), len(allowedTools()))
	}
}

// 契约：只有一台服务器时可以省略 server 参数；多台时必须点名，
// 且报错要把可选项列出来——模型据此重试，而不是瞎猜一个名字。
func TestService_Pick(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	if _, err := svc.pick(ctx, Scope{}, ""); err == nil {
		t.Fatal("一台都没有时必须报错")
	}

	one, err := svc.Create(ctx, Input{Name: "box", Host: "h", User: "u", Auth: "key"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.pick(ctx, Scope{}, "")
	if err != nil {
		t.Fatalf("只有一台时应允许省略: %v", err)
	}
	if got.ID != one.ID {
		t.Fatal("挑错了机器")
	}

	if _, err := svc.Create(ctx, Input{Name: "box2", Host: "h2", User: "u", Auth: "key"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.pick(ctx, Scope{}, "")
	if err == nil {
		t.Fatal("多台时省略必须报错")
	}
	if !strings.Contains(err.Error(), "box") || !strings.Contains(err.Error(), "box2") {
		t.Fatalf("报错要列出可选项，得到: %v", err)
	}

	if _, err := svc.pick(ctx, Scope{}, "BOX"); err != nil {
		t.Errorf("名字匹配应忽略大小写: %v", err)
	}
	_, err = svc.pick(ctx, Scope{}, "nope")
	if err == nil || !strings.Contains(err.Error(), "box") {
		t.Fatalf("找不到时也要列出可选项，得到: %v", err)
	}
}

// 契约：作用域锁定后只看得见那一台。这是 discord 频道绑定的执行点——
// 降级方向只能是「更少」，绝不能悄悄回退成全部可见。
func TestService_Visible_ScopeLock(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	a, _ := svc.Create(ctx, Input{Name: "a", Host: "h", User: "u", Auth: "key"})
	if _, err := svc.Create(ctx, Input{Name: "b", Host: "h", User: "u", Auth: "key"}); err != nil {
		t.Fatal(err)
	}

	all, err := svc.visible(ctx, Scope{})
	if err != nil || len(all) != 2 {
		t.Fatalf("不锁定时应全部可见，得到 %d 台 (%v)", len(all), err)
	}
	only, err := svc.visible(ctx, Scope{Only: a.ID})
	if err != nil || len(only) != 1 || only[0].ID != a.ID {
		t.Fatalf("锁定后只应看见那一台，得到 %+v (%v)", only, err)
	}
	// 锁定到一台不存在/已停用的：返回空，而不是回退成全部。
	gone, err := svc.visible(ctx, Scope{Only: 99999})
	if err != nil {
		t.Fatalf("visible: %v", err)
	}
	if len(gone) != 0 {
		t.Fatalf("锁定的机器没了应返回空（不能回退成全部可见），得到 %d 台", len(gone))
	}
}

// 契约：停用的服务器不进工具面的可见范围。
func TestService_Visible_SkipsDisabled(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, Input{Name: "on", Host: "h", User: "u", Auth: "key"}); err != nil {
		t.Fatal(err)
	}
	off, _ := svc.Create(ctx, Input{Name: "off", Host: "h", User: "u", Auth: "key"})
	if _, err := svc.Update(ctx, off.ID, Input{
		Name: "off", Host: "h", User: "u", Auth: "key", Disabled: ptr(true),
	}); err != nil {
		t.Fatal(err)
	}

	list, err := svc.visible(ctx, Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "on" {
		t.Fatalf("停用的不该出现在工具面里，得到 %+v", namesOf(list))
	}
}

// 契约：命令里的路径与 pattern 一律经过引用，且限流参数必须在。
// 少任何一道限流，一次调用都可能在几 GB 的日志目录上跑成事故。
func TestCommandBuilders_QuoteAndLimit(t *testing.T) {
	t.Run("ls 带条数上限且路径被引用", func(t *testing.T) {
		cmd := lsCmd(toolArgs{Path: "/srv/a b; rm -rf /"})
		if !strings.Contains(cmd, `'/srv/a b; rm -rf /'`) {
			t.Errorf("路径没被引用: %s", cmd)
		}
		if !strings.Contains(cmd, "head -n") {
			t.Errorf("缺条数上限: %s", cmd)
		}
	})

	t.Run("ls 的递归深度有硬顶", func(t *testing.T) {
		cmd := lsCmd(toolArgs{Path: "/srv", Depth: 99})
		if !strings.Contains(cmd, "-maxdepth 3") {
			t.Errorf("深度应被夹到 3: %s", cmd)
		}
	})

	t.Run("read 的 tail 与 offset 走不同分支", func(t *testing.T) {
		tail := readCmd(toolArgs{Path: "/var/log/x.log", Tail: 50})
		if !strings.Contains(tail, "tail -n 50") {
			t.Errorf("tail 分支不对: %s", tail)
		}
		mid := readCmd(toolArgs{Path: "/var/log/x.log", Offset: 100, Limit: 20})
		if !strings.Contains(mid, "sed -n '100,119p'") {
			t.Errorf("offset 分支不对: %s", mid)
		}
		// 两条都要先挡二进制并报文件大小。
		for _, c := range []string{tail, mid} {
			if !strings.Contains(c, "grep -qI") {
				t.Errorf("缺二进制检测: %s", c)
			}
			if !strings.Contains(c, "wc -l") {
				t.Errorf("缺总行数: %s", c)
			}
		}
	})

	t.Run("read 的行数有硬顶", func(t *testing.T) {
		cmd := readCmd(toolArgs{Path: "/x", Offset: 1, Limit: 99999})
		if !strings.Contains(cmd, "1,2000p") {
			t.Errorf("行数应被夹到 2000: %s", cmd)
		}
	})

	t.Run("grep 三道限流都在", func(t *testing.T) {
		cmd := grepCmd(toolArgs{Path: "/srv/log", Pattern: "error|fatal", Include: "*.log"})
		if !strings.Contains(cmd, "'error|fatal'") {
			t.Errorf("pattern 没被引用: %s", cmd)
		}
		if !strings.Contains(cmd, "-m 50") {
			t.Errorf("缺每文件命中上限: %s", cmd)
		}
		if !strings.Contains(cmd, "head -n") {
			t.Errorf("缺总行数兜底: %s", cmd)
		}
		if !strings.Contains(cmd, "nice -n 19") {
			t.Errorf("缺降优先级——生产机上会跟业务抢 CPU: %s", cmd)
		}
		if !strings.Contains(cmd, "--include='*.log'") {
			t.Errorf("include 没被引用: %s", cmd)
		}
	})

	t.Run("grep 的命中上限有硬顶", func(t *testing.T) {
		cmd := grepCmd(toolArgs{Path: "/x", Pattern: "a", MaxMatches: 99999})
		if !strings.Contains(cmd, "-m 200") {
			t.Errorf("命中上限应被夹到 200: %s", cmd)
		}
	})

	// 这条曾经漏掉过：修 zsh 分词时只改了 statsCmd，psCmd 没跟上，
	// 而当时的测试只覆盖 statsCmd，于是「以为修了」的东西带着错误的
	// commit message 进了库。多容器时才复现，单容器看着一切正常。
	t.Run("docker ps 取重启次数不靠 shell 分词", func(t *testing.T) {
		cmd := psCmd(toolArgs{Filter: "pp-"})
		if strings.Contains(cmd, "ids=$(") || strings.Contains(cmd, "$ids") {
			t.Errorf("容器 id 不能先存进变量再展开（zsh 不分词）: %s", cmd)
		}
		if !strings.Contains(cmd, "| xargs -r docker inspect") {
			t.Errorf("应当用管道喂 xargs: %s", cmd)
		}
		// 只列非 0 重启：健康状态在 ps 的 Status 列里已经有了，
		// 条件里带上它会让每个健康容器都被列出来。
		if !strings.Contains(cmd, "awk '$2 != 0 {print}'") {
			t.Errorf("只该列非 0 重启次数: %s", cmd)
		}
	})

	t.Run("docker 命令先探 docker 在不在", func(t *testing.T) {
		for _, cmd := range []string{
			psCmd(toolArgs{}),
			logsCmd(toolArgs{Container: "pp-server"}),
		} {
			if !strings.Contains(cmd, "command -v docker") {
				t.Errorf("没装 docker 时应给出人话而不是一堆 stderr: %s", cmd)
			}
		}
	})

	t.Run("docker logs 的过滤在服务端做", func(t *testing.T) {
		cmd := logsCmd(toolArgs{Container: "pp-server", Grep: "panic", Tail: 99999, Since: "30m"})
		if !strings.Contains(cmd, "| grep -E -- 'panic'") {
			t.Errorf("grep 应在服务端过滤（不是拉回来再筛）: %s", cmd)
		}
		if !strings.Contains(cmd, "| tail -n 2000") {
			t.Errorf("返回条数应被夹到 2000: %s", cmd)
		}
		if !strings.Contains(cmd, "--since '30m'") {
			t.Errorf("since 没被引用: %s", cmd)
		}
		if !strings.Contains(cmd, "'pp-server'") {
			t.Errorf("容器名没被引用: %s", cmd)
		}
	})

	// 契约：带 grep 时搜索窗口必须比返回条数大——`--tail 3 | grep` 是在
	// 最后三行里找，几乎必然一无所获。真机上踩过。
	t.Run("docker logs 带 grep 时窗口放大", func(t *testing.T) {
		cmd := logsCmd(toolArgs{Container: "c", Grep: "panic", Tail: 3})
		if strings.Contains(cmd, "--tail 3 ") {
			t.Errorf("搜索窗口不该等于返回条数: %s", cmd)
		}
		if !strings.Contains(cmd, "| tail -n 3") {
			t.Errorf("最终仍应只返回 3 条: %s", cmd)
		}
		// 不带 grep 时窗口就是返回条数，不必多拉。
		plain := logsCmd(toolArgs{Container: "c", Tail: 3})
		if !strings.Contains(plain, "--tail 3 ") {
			t.Errorf("不带 grep 时窗口应等于返回条数: %s", plain)
		}
	})
}

// 契约：环境变量只能给名字。容器 env 里躺着数据库密码、API key 与 token
// ——一次 inspect 就能把它们全带进模型上下文，而想确认某个配置项在不在，
// 看名字就够了。
func TestInspectCmd_RedactsEnvValues(t *testing.T) {
	cmd := inspectCmd(toolArgs{Container: "pp-server"})
	if !strings.Contains(cmd, "cut -d= -f1") {
		t.Errorf("环境变量必须只取名字（cut 掉等号后的值）: %s", cmd)
	}
	if !strings.Contains(cmd, "'pp-server'") {
		t.Errorf("容器名没被引用: %s", cmd)
	}
	// compose 标签是代码与服务器之间的桥，不能漏。
	if !strings.Contains(cmd, "com.docker.compose.project.working_dir") {
		t.Errorf("缺 compose 的 working_dir 标签: %s", cmd)
	}
}

// 契约：P1 的几个工具同样要有降级——不是每台机器都有 systemd、
// 都装了 docker、都用 GNU 的 ps。
func TestP1Commands_Degrade(t *testing.T) {
	if !strings.Contains(journalCmd(toolArgs{}), "command -v journalctl") {
		t.Error("没有 systemd 的机器上应给人话")
	}
	if !strings.Contains(statsCmd(toolArgs{}), "command -v docker") {
		t.Error("没装 docker 的机器上应给人话")
	}
	if !strings.Contains(portsCmd, "netstat") {
		t.Error("没有 ss 的老系统应退回 netstat")
	}
	if !strings.Contains(psHostCmd(toolArgs{}), "ps aux") {
		t.Error("busybox 的 ps 不认 --sort，应有退路")
	}
}

// 契约：docker_stats 的容器筛选同样不能靠 shell 分词（远端可能是 zsh）。
func TestStatsCmd_NoWordSplitting(t *testing.T) {
	cmd := statsCmd(toolArgs{Filter: "pp-"})
	if !strings.Contains(cmd, "| xargs -r docker stats") {
		t.Errorf("容器 id 要用管道喂 xargs，不能指望 shell 分词: %s", cmd)
	}
	if !strings.Contains(cmd, "--no-stream") {
		t.Errorf("必须 --no-stream，否则它会一直刷: %s", cmd)
	}
	if !strings.Contains(cmd, "name='pp-'") {
		t.Errorf("筛选串没被引用: %s", cmd)
	}
	// 不带筛选时不必绕 xargs。
	if strings.Contains(statsCmd(toolArgs{}), "xargs") {
		t.Error("没有筛选条件时应直接 docker stats")
	}
}

// 契约：nice 只能跟一条实在的命令。
//
// 两个真机上踩过的形状：`nice -n 19 (…)` 在 zsh 里直接 parse error；
// `nice -n 19` 后面跟 if 语句时 nice 拿不到命令，自己报错、降优先级失效。
func TestNice_NeverPrecedesCompound(t *testing.T) {
	cmds := map[string]string{
		"ls":      lsCmd(toolArgs{Path: "/srv"}),
		"read":    readCmd(toolArgs{Path: "/x", Tail: 5}),
		"grep":    grepCmd(toolArgs{Path: "/x", Pattern: "a"}),
		"ps":      psHostCmd(toolArgs{}),
		"journal": journalCmd(toolArgs{}),
		"logs":    logsCmd(toolArgs{Container: "c"}),
	}
	for name, cmd := range cmds {
		for _, bad := range []string{"nice -n 19 (", "nice -n 19 if ", "nice -n 19\n"} {
			if strings.Contains(cmd, bad) {
				t.Errorf("%s: nice 后面跟了复合语句 %q，降优先级会失效: %s", name, bad, cmd)
			}
		}
	}
}

// 契约：能力探测不能写成「管道 || 备用」——管道的退出码取自最后一环
// （通常是 head，永远 0），`||` 那半边成了死代码，在 busybox 机器上
// 表现为一片空输出。
func TestFallback_NotAfterPipe(t *testing.T) {
	for name, cmd := range map[string]string{
		"ls": lsCmd(toolArgs{Path: "/srv"}),
		"ps": psHostCmd(toolArgs{}),
	} {
		if strings.Contains(cmd, "| head -n") && strings.Contains(cmd, "|| ") {
			t.Errorf("%s: 备用分支跟在管道之后，永远不会执行: %s", name, cmd)
		}
		if !strings.Contains(cmd, "if ") {
			t.Errorf("%s: 应当先探测能力再选命令: %s", name, cmd)
		}
	}
}
