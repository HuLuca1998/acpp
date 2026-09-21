// 技能库的端点（磁盘为事实源：源目录 skills/ + skillpack 分发链接）。
//
// 从 index.ts 拆出来是因为那个文件到了行数硬线，而这一族本来就自成一块：
// 技能本身、附属文件、脚本试运行、使用统计与换设备搬家的导入导出，共用
// /skills 这一棵路径。展开进 `api`，调用方仍写 `api.skills.list()`。

import type {
  Paged,
  PageQuery,
  Skill,
  SkillCreateInput,
  SkillDetail,
  SkillFile,
  SkillFileContent,
  SkillImportResult,
  SkillScript,
  SkillScriptRunInput,
  SkillScriptRunResult,
  SkillUpdateInput,
  SkillUsage,
} from "@/types/acp"

import { ApiError, BASE, pageQuery, request } from "./core"

export const skillsApi = {
  skills: {
    /** enabled 用 "1" / "0" 三态，不传不过滤（pageQuery 会把空串丢掉）。 */
    list: (params?: Partial<PageQuery> & { q?: string; enabled?: string }) =>
      request<Paged<Skill>>(`/skills${pageQuery(params)}`),
    get: (name: string) => request<SkillDetail>(`/skills/${name}`),
    create: (input: SkillCreateInput) =>
      request<SkillDetail>("/skills", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    update: (name: string, input: SkillUpdateInput) =>
      request<SkillDetail>(`/skills/${name}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    remove: (name: string) =>
      request<{ deleted: boolean }>(`/skills/${name}`, { method: "DELETE" }),

    files: (name: string) => request<Paged<SkillFile>>(`/skills/${name}/files`),
    file: (name: string, path: string) =>
      request<SkillFileContent>(`/skills/${name}/files/${path}`),
    putFile: (name: string, path: string, content: string) =>
      request<SkillFile>(`/skills/${name}/files/${path}`, {
        method: "PUT",
        body: JSON.stringify({ content }),
      }),
    removeFile: (name: string, path: string) =>
      request<{ deleted: boolean }>(`/skills/${name}/files/${path}`, {
        method: "DELETE",
      }),

    /**
     * 导出地址（整库或单个技能）。搬家是「把文件拿走」，交给浏览器原生下载
     * 最省事：带 cookie、不占内存、进度条是系统的——不走 fetch 转 blob。
     */
    exportUrl: (name?: string) =>
      name ? `${BASE}/skills/${name}/export` : `${BASE}/skills/export`,
    /** 导入 zip：还原回来的技能一律停用，由用户在页面确认后再打开。 */
    importZip: async (file: File) => {
      const body = new FormData()
      body.append("file", file)
      // 不设 Content-Type：multipart 的 boundary 得让浏览器自己填。
      const res = await fetch(`${BASE}/skills/import`, { method: "POST", body })
      const json = (await res.json()) as {
        data?: SkillImportResult
        error?: string
      }
      if (!res.ok) throw new ApiError(res.status, json.error ?? res.statusText)
      return json.data as SkillImportResult
    },

    usage: () => request<Paged<SkillUsage>>("/skills/usage"),

    scripts: (name: string) =>
      request<Paged<SkillScript>>(`/skills/${name}/scripts`),
    runScript: (name: string, input: SkillScriptRunInput) =>
      request<SkillScriptRunResult>(`/skills/${name}/scripts/run`, {
        method: "POST",
        body: JSON.stringify(input),
      }),
  },
}
