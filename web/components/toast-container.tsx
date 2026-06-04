'use client'

import { useToastContext } from './toast-context'
import { AlertCircle, CheckCircle, Info, AlertTriangle, X } from 'lucide-react'

const toastConfig = {
  success: {
    icon: CheckCircle,
    bgColor: 'bg-green-50',
    borderColor: 'border-green-200',
    titleColor: 'text-green-900',
    descColor: 'text-green-800',
    closeColor: 'hover:bg-green-100',
  },
  error: {
    icon: AlertCircle,
    bgColor: 'bg-red-50',
    borderColor: 'border-red-200',
    titleColor: 'text-red-900',
    descColor: 'text-red-800',
    closeColor: 'hover:bg-red-100',
  },
  warning: {
    icon: AlertTriangle,
    bgColor: 'bg-yellow-50',
    borderColor: 'border-yellow-200',
    titleColor: 'text-yellow-900',
    descColor: 'text-yellow-800',
    closeColor: 'hover:bg-yellow-100',
  },
  info: {
    icon: Info,
    bgColor: 'bg-blue-50',
    borderColor: 'border-blue-200',
    titleColor: 'text-blue-900',
    descColor: 'text-blue-800',
    closeColor: 'hover:bg-blue-100',
  },
}

export function ToastContainer() {
  const { toasts, removeToast } = useToastContext()

  return (
    <div className="fixed bottom-4 right-4 z-50 space-y-2 max-w-sm pointer-events-none">
      {toasts.map((toast) => {
        const config = toastConfig[toast.type]
        const Icon = config.icon

        return (
          <div
            key={toast.id}
            className={`${config.bgColor} ${config.borderColor} border rounded-lg p-4 shadow-lg pointer-events-auto animate-in slide-in-from-right-4 fade-in duration-200`}
          >
            <div className="flex items-start gap-3">
              <Icon className={`w-5 h-5 ${config.titleColor} flex-shrink-0 mt-0.5`} />
              <div className="flex-1 min-w-0">
                <p className={`font-semibold text-sm ${config.titleColor}`}>{toast.title}</p>
                {toast.description && <p className={`text-sm mt-1 ${config.descColor}`}>{toast.description}</p>}
              </div>
              <button
                onClick={() => removeToast(toast.id)}
                className={`flex-shrink-0 ml-2 p-1 rounded transition-colors ${config.closeColor}`}
              >
                <X className="w-4 h-4" />
              </button>
            </div>
          </div>
        )
      })}
    </div>
  )
}
