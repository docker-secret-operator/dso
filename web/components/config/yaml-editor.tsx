'use client'

import Editor, { loader } from '@monaco-editor/react'
import * as monaco from 'monaco-editor'

// Self-host Monaco: use the bundled monaco-editor instead of the default CDN
// loader, and route the editor web worker to a locally-bundled file. This keeps
// the editor working in air-gapped deployments with no runtime CDN dependency.
if (typeof window !== 'undefined') {
  loader.config({ monaco })
  ;(self as unknown as { MonacoEnvironment: unknown }).MonacoEnvironment = {
    getWorker() {
      try {
        return new Worker(new URL('monaco-editor/esm/vs/editor/editor.worker.js', import.meta.url))
      } catch {
        // No worker available — Monaco still renders with synchronous YAML
        // tokenization. Avoids any CDN fallback / hard failure.
        return undefined as unknown as Worker
      }
    },
  }
}

interface YamlEditorProps {
  value: string
  onChange: (value: string) => void
  readOnly?: boolean
  height?: string
}

export default function YamlEditor({ value, onChange, readOnly = false, height = '420px' }: YamlEditorProps) {
  return (
    <div className="rounded-lg overflow-hidden border border-white/[0.08]">
      <Editor
        height={height}
        defaultLanguage="yaml"
        language="yaml"
        theme="vs-dark"
        value={value}
        onChange={(v) => onChange(v ?? '')}
        options={{
          readOnly,
          fontFamily: 'JetBrains Mono, Fira Code, monospace',
          fontSize: 13,
          minimap: { enabled: false },
          lineNumbers: 'on',
          folding: true,
          scrollBeyondLastLine: false,
          automaticLayout: true,
          tabSize: 2,
          renderWhitespace: 'none',
          smoothScrolling: true,
        }}
      />
    </div>
  )
}
