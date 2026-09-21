import { useState } from "react"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { toast } from "sonner"
import type { ColumnDef } from "@tanstack/react-table"
import {
  CopyIcon,
  HardDriveIcon,
  KeyRoundIcon,
  PencilIcon,
  PlusIcon,
  Trash2Icon,
} from "lucide-react"

import { api } from "@/lib/api"
import type { SSHKey } from "@/types/acp"
import { Hint } from "@/components/hint"
import { ListPageHeader } from "@/components/list-page-header"
import { ListPageStates } from "@/components/list-page-states"
import { SSHKeyDialog } from "@/components/servers/ssh-key-dialog"
import { usePagedData } from "@/hooks/use-paged-data"
import { DataTable } from "@/components/data-table/data-table"
import {
  SearchBar,
  SearchText,
} from "@/components/data-table/data-table-search"
import { useSearchDraft } from "@/hooks/use-search-draft"
import { DataTableHeader } from "@/components/data-table/data-table-header"
import type { dataTableFeatures } from "@/components/data-table/data-table-features"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"

type KeyColumn = ColumnDef<typeof dataTableFeatures, SSHKey, unknown>

/**
 * 私钥页：一把钥匙开好几台机器是常态，所以私钥独立成库，服务器引用它。
 *
 * 私钥**内容**存在库里而不是路径——路径只在这台机器上有意义，而连接配置
 * 要能整套搬到另一台电脑。列表显示指纹与公钥：前者用来认出是哪一把（名字
 * 可以起得很随意），后者一键复制去装进目标机器的 authorized_keys。
 */
export function SSHKeys() {
  const { t } = useTranslation()
  const search = useSearchDraft({ q: "" })
  const { values } = search
  const {
    items: keys,
    total,
    error,
    page,
    pageSize,
    sorting,
    setPage,
    setPageSize,
    setSorting,
    fetching,
    reload,
    remove: dropRow,
  } = usePagedData((params) => api.sshKeys.list({ ...params, q: values.q }), {
    deps: [values],
  })
  const submitSearch = () => {
    search.commit()
    setPage(1)
  }
  const resetSearch = () => {
    search.reset()
    setPage(1)
  }

  // undefined = 对话框关着；null = 新建；对象 = 编辑那一把。
  const [editing, setEditing] = useState<SSHKey | null | undefined>(undefined)
  const [deleting, setDeleting] = useState<SSHKey | null>(null)

  async function copyPublicKey(key: SSHKey) {
    try {
      await navigator.clipboard.writeText(key.publicKey)
      toast.success(t("sshKeys.publicKeyCopied"))
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  async function confirmDelete() {
    if (!deleting) return
    try {
      await api.sshKeys.remove(deleting.id)
      dropRow(deleting.id)
      toast.success(t("sshKeys.deleted"))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setDeleting(null)
    }
  }

  return (
    <div className="flex flex-col gap-4 p-4 lg:p-6">
      <ListPageHeader
        title={t("sshKeys.title")}
        total={keys ? total : undefined}
      />
      <DataTable
        columns={keyColumns(t, setEditing, setDeleting, copyPublicKey)}
        data={error ? null : keys}
        total={total}
        page={page}
        pageSize={pageSize}
        sorting={sorting}
        search={
          <SearchBar onSearch={submitSearch} onReset={resetSearch}>
            <SearchText
              value={search.draft.q}
              onChange={(v) => search.set("q", v)}
              placeholder={t("sshKeys.searchPlaceholder")}
            />
          </SearchBar>
        }
        fetching={fetching}
        actions={
          <Button size="sm" onClick={() => setEditing(null)}>
            <PlusIcon data-icon="inline-start" />
            {t("sshKeys.add")}
          </Button>
        }
        onReload={reload}
        onPage={setPage}
        onPageSize={setPageSize}
        onSorting={setSorting}
        onRowClick={setEditing}
        empty={
          <ListPageStates
            icon={<KeyRoundIcon />}
            error={error}
            loading={keys === null}
            emptyTitle={t("sshKeys.empty")}
            emptyHint={t("sshKeys.emptyHint")}
            emptyAction={
              <Button size="sm" onClick={() => setEditing(null)}>
                <PlusIcon data-icon="inline-start" />
                {t("sshKeys.add")}
              </Button>
            }
          />
        }
      />

      {editing !== undefined && (
        <SSHKeyDialog
          sshKey={editing}
          onClose={() => setEditing(undefined)}
          onSaved={() => {
            setEditing(undefined)
            reload()
          }}
        />
      )}

      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("sshKeys.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("sshKeys.deleteBody", { name: deleting?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={confirmDelete}>
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function keyColumns(
  t: TFunction,
  onEdit: (key: SSHKey) => void,
  onDelete: (key: SSHKey) => void,
  onCopy: (key: SSHKey) => void
): KeyColumn[] {
  return [
    {
      accessorKey: "name",
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("sshKeys.name")} />
      ),
      meta: { pin: "left" },
      cell: ({ row }) => (
        <span className="font-mono font-medium">{row.original.name}</span>
      ),
    },
    {
      accessorKey: "fingerprint",
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("sshKeys.fingerprint")} />
      ),
      // 指纹是认出「是哪一把」的唯一可靠依据，名字可以起得很随意。
      cell: ({ row }) => (
        <span
          title={row.original.fingerprint}
          className="line-clamp-1 max-w-[min(24rem,20vw)] font-mono text-xs text-muted-foreground"
        >
          {row.original.fingerprint}
        </span>
      ),
    },
    {
      accessorKey: "note",
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("sshKeys.note")} />
      ),
      cell: ({ row }) => (
        <span
          title={row.original.note}
          className="line-clamp-1 max-w-[min(22rem,18vw)] text-muted-foreground"
        >
          {row.original.note}
        </span>
      ),
    },
    {
      id: "usedBy",
      header: () => null,
      // 用这把钥匙的服务器台数。它决定了这把能不能删，而那件事本来只有在
      // 点了删除被拒绝时才会被发现。
      cell: ({ row }) =>
        row.original.usedBy > 0 ? (
          <Hint label={t("sshKeys.usedBy", { count: row.original.usedBy })}>
            <Badge variant="outline" className="gap-1 text-[11px]">
              <HardDriveIcon className="size-3" />
              {row.original.usedBy}
            </Badge>
          </Hint>
        ) : null,
    },
    {
      id: "passphrase",
      header: () => null,
      cell: ({ row }) =>
        row.original.hasPassphrase ? (
          <Badge variant="secondary" className="text-[11px]">
            {t("sshKeys.hasPassphrase")}
          </Badge>
        ) : null,
    },
    {
      id: "actions",
      header: () => null,
      meta: { pin: "right" },
      cell: ({ row }) => (
        // 行本身可点开编辑，这几个按钮不能把点击冒泡上去。
        <div
          className="flex justify-end gap-1"
          onClick={(e) => e.stopPropagation()}
        >
          <Hint label={t("sshKeys.copyPublicKey")}>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("sshKeys.copyPublicKey")}
              onClick={() => onCopy(row.original)}
            >
              <CopyIcon />
            </Button>
          </Hint>
          <Hint label={t("sshKeys.edit")}>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("sshKeys.edit")}
              onClick={() => onEdit(row.original)}
            >
              <PencilIcon />
            </Button>
          </Hint>
          {/* 被引用时直接禁用而不是点了再报错——按钮能按却必然失败，
              是最没必要的一次往返。 */}
          <Hint
            label={
              row.original.usedBy > 0
                ? t("sshKeys.deleteInUse")
                : t("common.delete")
            }
            align="end"
          >
            <Button
              variant="ghost"
              size="icon"
              className="text-muted-foreground hover:text-destructive"
              aria-label={t("common.delete")}
              disabled={row.original.usedBy > 0}
              onClick={() => onDelete(row.original)}
            >
              <Trash2Icon />
            </Button>
          </Hint>
        </div>
      ),
    },
  ]
}
