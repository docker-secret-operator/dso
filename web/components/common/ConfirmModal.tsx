'use client'

interface ConfirmModalProps {
  title: string
  message: string
  confirmLabel?: string
  onConfirm: () => void
  onCancel: () => void
}

export function ConfirmModal({
  title,
  message,
  confirmLabel = 'Confirm',
  onConfirm,
  onCancel,
}: ConfirmModalProps) {
  return (
    <>
      <div
        className="fixed inset-0 bg-black/60 backdrop-blur-sm z-50"
        onClick={onCancel}
      />
      <div className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 z-50 w-full max-w-sm px-4">
        <div className="bg-[#111827] border border-white/[0.09] rounded-xl p-6 shadow-2xl">
          <h3 className="text-sm font-semibold text-slate-100 mb-2">{title}</h3>
          <p className="text-sm text-slate-400 mb-6 leading-relaxed">{message}</p>
          <div className="flex gap-3 justify-end">
            <button
              onClick={onCancel}
              className="px-4 py-2 text-xs rounded-lg border border-white/[0.09] text-slate-300 hover:bg-white/5 transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={() => { onConfirm() }}
              className="px-4 py-2 text-xs rounded-lg bg-red-600 text-white hover:bg-red-500 transition-colors"
            >
              {confirmLabel}
            </button>
          </div>
        </div>
      </div>
    </>
  )
}
