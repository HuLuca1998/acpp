// 连接类端点：远程服务器（adr-019）与数据库数据源（adr-008）。
//
// 从 api.ts 拆出来是因为那个文件到了行数硬线，而这两块正好是同一件事的
// 两面——连哪台机器、连哪个库，且服务器就是数据源的拨号跳板。展开进
// `api` 对象，调用方仍然写 `api.servers.list()`。

import type {
  DataSource,
  DataSourceInput,
  DataSourceTest,
  DataSourceUri,
  DbDatabase,
  DbTable,
  DbTableDetail,
  Paged,
  PageQuery,
  Server,
  ServerInput,
  SqlExecResult,
} from "@/types/acp"

import { pageQuery, request } from "./core"

export const connectionsApi = {
  /**
   * 远程服务器（adr-019）。owner 专属：这些记录里躺着生产机的 SSH 凭证。
   * 它同时是数据源的拨号跳板与 AI 只读观察的目标——两处用的是同一份配置。
   */
  servers: {
    // 服务器是个位数量级的配置，后端一次返回全部；分页参数照发不误，
    // 好让列表页与别处共用同一个分页 hook（后端忽略它们）。
    list: (params?: Partial<PageQuery>) =>
      request<Paged<Server>>(`/servers${pageQuery(params)}`),
    get: (id: number) => request<Server>(`/servers/${id}`),
    create: (input: ServerInput) =>
      request<Server>("/servers", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    update: (id: number, input: ServerInput) =>
      request<Server>(`/servers/${id}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    remove: (id: number) => request<null>(`/servers/${id}`, { method: "DELETE" }),
    /** 测一份还没保存的配置（新建对话框里的按钮）。 */
    probe: (input: ServerInput) =>
      request<{ version: string }>("/servers/probe", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    /**
     * 测一条已存的记录。传空对象表示「就测这条」；带表单内容则先合并
     * 再测——编辑到一半想验一下，不该逼用户先保存。
     */
    test: (id: number, input?: ServerInput) =>
      request<{ version: string }>(`/servers/${id}/test`, {
        method: "POST",
        body: JSON.stringify(input ?? {}),
      }),
  },

  /**
   * 数据库数据源（adr-008）。管理面是全量的（owner 专属）；会话面在
   * sessions.datasources 下，只给当前项目的那几条——两个入口刻意分开，
   * 免得在会话里误用别的项目的连接。
   */
  datasources: {
    list: (params?: Partial<PageQuery>) =>
      request<Paged<DataSource>>(`/datasources${pageQuery(params)}`),
    /** 配置页选库用：列出这组连接参数能看到的全部库（连接还没绑定库）。 */
    probeDatabases: (input: DataSourceInput & { id?: number }) =>
      request<DbDatabase[]>("/datasources/probe-databases", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    /** SSH 页签单独测隧道，不碰 MySQL；失败形状同 test。 */
    probeSSH: (input: DataSourceInput & { id?: number }) =>
      request<DataSourceTest>("/datasources/probe-ssh", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    get: (id: number) => request<DataSource>(`/datasources/${id}`),
    create: (input: DataSourceInput) =>
      request<DataSource>("/datasources", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    update: (id: number, input: DataSourceInput) =>
      request<DataSource>(`/datasources/${id}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    remove: (id: number) =>
      request<null>(`/datasources/${id}`, { method: "DELETE" }),
    /** 拨一次真连接确认配置可用，失败不抛异常而是返回 ok:false + 原话。 */
    test: (id: number) =>
      request<DataSourceTest>(`/datasources/${id}/test`, { method: "POST" }),
    /**
     * 取明文密码（owner 专属）。只在两处用：密码框的「看一眼」，
     * 与「复制连接」——同一台库上开几个连接时账号密码是同一套，
     * 让人再抄一遍反而更容易抄错。
     */
    secret: (id: number) =>
      request<{ password: string }>(`/datasources/${id}/secret`),
    /** 导出连接 URI（Navicat 与通用两种写法，**含密码**）。 */
    uri: (id: number) => request<DataSourceUri>(`/datasources/${id}/uri`),
    databases: (id: number) =>
      request<DbDatabase[]>(`/datasources/${id}/databases`),
    tables: (id: number, database: string) =>
      request<DbTable[]>(
        `/datasources/${id}/tables?database=${encodeURIComponent(database)}`
      ),
    schema: (id: number, database: string, table: string) =>
      request<DbTableDetail>(
        `/datasources/${id}/schema?database=${encodeURIComponent(database)}&table=${encodeURIComponent(table)}`
      ),
    /** 执行一段 SQL，可含多条语句（按顺序执行、遇错即停）。 */
    query: (
      id: number,
      input: { database?: string; sql: string; maxRows?: number }
    ) =>
      request<SqlExecResult>(`/datasources/${id}/query`, {
        method: "POST",
        body: JSON.stringify(input),
      }),
  },
}
