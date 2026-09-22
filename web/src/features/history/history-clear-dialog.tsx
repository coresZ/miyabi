import { LoaderCircleIcon, Trash2Icon } from 'lucide-react'

import { InlineError } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'

export function HistoryClearDialog({
  clearMode,
  selectedCount,
  isPending,
  error,
  onClose,
  onConfirm
}: {
  clearMode: 'all' | 'selected' | null
  selectedCount: number
  isPending: boolean
  error?: Error | null
  onClose: () => void
  onConfirm: () => void
}) {
  return (
    <Dialog
      open={clearMode !== null}
      onOpenChange={open => {
        if (!open && !isPending) onClose()
      }}
    >
      <DialogContent showCloseButton={!isPending}>
        <DialogHeader>
          <DialogTitle>清除观看历史</DialogTitle>
          <DialogDescription>
            {clearMode === 'all'
              ? '清除当前媒体目录的全部观看记录和播放进度？'
              : `清除选中的 ${selectedCount} 条观看记录和播放进度？`}
            影片文件和已观看状态会保留。
          </DialogDescription>
        </DialogHeader>
        {error ? <InlineError>{error.message}</InlineError> : null}
        <DialogFooter>
          <Button type="button" variant="outline" disabled={isPending} onClick={onClose}>
            取消
          </Button>
          <Button
            type="button"
            variant="destructive"
            disabled={isPending || (clearMode === 'selected' && selectedCount === 0)}
            onClick={onConfirm}
          >
            {isPending ? <LoaderCircleIcon className="animate-spin" /> : <Trash2Icon />}
            确认清除
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
