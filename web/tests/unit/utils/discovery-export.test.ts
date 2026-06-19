import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  exportContainersToCSV,
  exportContainersToJSON,
  downloadExport,
} from '@/lib/utils/discovery-export'

describe('Discovery Export Utilities', () => {
  describe('exportContainersToCSV', () => {
    it('should return empty string for empty containers array', () => {
      const result = exportContainersToCSV([])
      expect(result).toBe('')
    })

    it('should generate correct CSV headers', () => {
      const containers = [
        {
          container_name: 'test',
          image: 'image:1.0',
          status: 'running',
          dso_awareness: null,
        },
      ]
      const result = exportContainersToCSV(containers)
      const lines = result.split('\n')
      const headers = lines[0]

      expect(headers).toContain('Container Name')
      expect(headers).toContain('Image')
      expect(headers).toContain('Status')
      expect(headers).toContain('Classification')
      expect(headers).toContain('Managed Secrets')
      expect(headers).toContain('Missing Mappings')
    })

    it('should export single container with DSO awareness', () => {
      const containers = [
        {
          container_name: 'app-1',
          image: 'myapp:1.0',
          status: 'running',
          dso_awareness: {
            status: 'managed',
            managed_secrets: ['SECRET1', 'SECRET2'],
            missing_mappings: ['MAP1'],
          },
        },
      ]
      const result = exportContainersToCSV(containers)
      const lines = result.split('\n')

      expect(lines.length).toBe(2)
      expect(lines[1]).toContain('app-1')
      expect(lines[1]).toContain('myapp:1.0')
      expect(lines[1]).toContain('running')
      expect(lines[1]).toContain('managed')
      expect(lines[1]).toContain('2')
      expect(lines[1]).toContain('1')
    })

    it('should handle containers without DSO awareness', () => {
      const containers = [
        {
          container_name: 'legacy-app',
          image: 'legacy:2.0',
          status: 'exited',
          dso_awareness: null,
        },
      ]
      const result = exportContainersToCSV(containers)
      const lines = result.split('\n')

      expect(lines[1]).toContain('legacy-app')
      expect(lines[1]).toContain('unmanaged')
      expect(lines[1]).toContain('0')
    })

    it('should properly escape CSV special characters', () => {
      const containers = [
        {
          container_name: 'test,app',
          image: 'image"with"quotes',
          status: 'running',
          dso_awareness: null,
        },
      ]
      const result = exportContainersToCSV(containers)
      const lines = result.split('\n')

      expect(lines[1]).toContain('"test,app"')
      expect(lines[1]).toContain('image""with""quotes')
    })

    it('should handle newlines in container data', () => {
      const containers = [
        {
          container_name: 'test\napp',
          image: 'image:1.0',
          status: 'running',
          dso_awareness: null,
        },
      ]
      const result = exportContainersToCSV(containers)

      expect(result).toContain('test\napp')
    })

    it('should export multiple containers', () => {
      const containers = [
        {
          container_name: 'app-1',
          image: 'app:1.0',
          status: 'running',
          dso_awareness: { status: 'managed', managed_secrets: [], missing_mappings: [] },
        },
        {
          container_name: 'app-2',
          image: 'app:2.0',
          status: 'paused',
          dso_awareness: null,
        },
        {
          container_name: 'app-3',
          image: 'app:3.0',
          status: 'stopped',
          dso_awareness: { status: 'unmanaged', managed_secrets: ['S1'], missing_mappings: ['M1', 'M2'] },
        },
      ]
      const result = exportContainersToCSV(containers)
      const lines = result.split('\n')

      expect(lines.length).toBe(4)
      expect(lines[1]).toContain('app-1')
      expect(lines[2]).toContain('app-2')
      expect(lines[3]).toContain('app-3')
    })

    it('should handle empty managed_secrets and missing_mappings arrays', () => {
      const containers = [
        {
          container_name: 'empty-app',
          image: 'empty:1.0',
          status: 'running',
          dso_awareness: {
            status: 'managed',
            managed_secrets: [],
            missing_mappings: [],
          },
        },
      ]
      const result = exportContainersToCSV(containers)
      const lines = result.split('\n')

      expect(lines[1]).toContain('0')
    })
  })

  describe('exportContainersToJSON', () => {
    it('should return empty array JSON for empty containers', () => {
      const result = exportContainersToJSON([])
      expect(result).toBe('[]')
    })

    it('should export single container as JSON', () => {
      const containers = [
        {
          container_name: 'test-app',
          image: 'test:1.0',
          status: 'running',
        },
      ]
      const result = exportContainersToJSON(containers)
      const parsed = JSON.parse(result)

      expect(Array.isArray(parsed)).toBe(true)
      expect(parsed.length).toBe(1)
      expect(parsed[0].container_name).toBe('test-app')
    })

    it('should maintain proper JSON formatting with indentation', () => {
      const containers = [{ container_name: 'app', image: 'img:1.0' }]
      const result = exportContainersToJSON(containers)

      expect(result).toContain('\n')
      expect(result).toContain('  ')
      expect(result.startsWith('[\n')).toBe(true)
    })

    it('should export multiple containers with all properties', () => {
      const containers = [
        {
          container_name: 'app-1',
          image: 'app:1.0',
          status: 'running',
          dso_awareness: { status: 'managed', managed_secrets: ['S1'], missing_mappings: [] },
        },
        {
          container_name: 'app-2',
          image: 'app:2.0',
          status: 'stopped',
          dso_awareness: null,
        },
      ]
      const result = exportContainersToJSON(containers)
      const parsed = JSON.parse(result)

      expect(parsed.length).toBe(2)
      expect(parsed[0].container_name).toBe('app-1')
      expect(parsed[1].container_name).toBe('app-2')
    })

    it('should preserve complex nested objects', () => {
      const containers = [
        {
          container_name: 'complex-app',
          image: 'complex:1.0',
          status: 'running',
          dso_awareness: {
            status: 'managed',
            managed_secrets: ['S1', 'S2', 'S3'],
            missing_mappings: ['M1', 'M2'],
            additional_field: { nested: 'value' },
          },
          custom_field: { level1: { level2: 'value' } },
        },
      ]
      const result = exportContainersToJSON(containers)
      const parsed = JSON.parse(result)

      expect(parsed[0].dso_awareness.managed_secrets.length).toBe(3)
      expect(parsed[0].custom_field.level1.level2).toBe('value')
    })

    it('should handle special characters in JSON', () => {
      const containers = [
        {
          container_name: 'app"with"quotes',
          image: 'image\nwith\nnewlines',
          status: 'running',
        },
      ]
      const result = exportContainersToJSON(containers)
      const parsed = JSON.parse(result)

      expect(parsed[0].container_name).toBe('app"with"quotes')
      expect(parsed[0].image).toContain('\n')
    })
  })

  describe('downloadExport', () => {
    let createElementSpy: any
    let appendChildSpy: any
    let removeChildSpy: any
    let revokeObjectURLSpy: any

    beforeEach(() => {
      createElementSpy = vi.spyOn(document, 'createElement')
      appendChildSpy = vi.spyOn(document.body, 'appendChild')
      removeChildSpy = vi.spyOn(document.body, 'removeChild')
      revokeObjectURLSpy = vi.spyOn(URL, 'revokeObjectURL')
    })

    afterEach(() => {
      vi.clearAllMocks()
    })

    it('should create anchor element', () => {
      downloadExport('test data', 'export.csv', 'text/csv')

      expect(createElementSpy).toHaveBeenCalledWith('a')
    })

    it('should set download attribute on link', () => {
      downloadExport('test data', 'export.csv', 'text/csv')

      const link = createElementSpy.mock.results[0]?.value
      expect(link?.download).toBe('export.csv')
    })

    it('should set href on link', () => {
      downloadExport('test data', 'export.csv', 'text/csv')

      const link = createElementSpy.mock.results[0]?.value
      expect(link?.href).toBeTruthy()
      expect(link?.href).toMatch(/^blob:/)
    })

    it('should append link to document body', () => {
      downloadExport('data', 'test.json', 'application/json')

      expect(appendChildSpy).toHaveBeenCalled()
    })

    it('should trigger click on link', () => {
      const clickSpy = vi.fn()
      const originalCreateElement = document.createElement.bind(document)
      vi.spyOn(document, 'createElement').mockImplementation((tag) => {
        const element = originalCreateElement(tag)
        if (tag === 'a') {
          element.click = clickSpy
        }
        return element
      })

      downloadExport('data', 'test.txt', 'text/plain')

      expect(clickSpy).toHaveBeenCalled()
    })

    it('should remove link from document body', () => {
      downloadExport('data', 'test.csv', 'text/csv')

      expect(removeChildSpy).toHaveBeenCalled()
    })

    it('should revoke object URL after download', () => {
      downloadExport('data', 'test.csv', 'text/csv')

      expect(revokeObjectURLSpy).toHaveBeenCalled()
    })

    it('should handle CSV export with proper filename', () => {
      const csvData = '"Container Name","Image"\n"app1","img1"'

      downloadExport(csvData, 'containers.csv', 'text/csv')

      const link = createElementSpy.mock.results[createElementSpy.mock.results.length - 1]?.value
      expect(link?.download).toBe('containers.csv')
    })

    it('should handle JSON export with proper filename', () => {
      const jsonData = JSON.stringify([{ container_name: 'app1' }], null, 2)

      downloadExport(jsonData, 'containers.json', 'application/json')

      const link = createElementSpy.mock.results[createElementSpy.mock.results.length - 1]?.value
      expect(link?.download).toBe('containers.json')
    })

    it('should handle empty data export', () => {
      downloadExport('', 'empty.csv', 'text/csv')

      expect(appendChildSpy).toHaveBeenCalled()
      expect(removeChildSpy).toHaveBeenCalled()
    })

    it('should handle filename with special characters', () => {
      downloadExport('data', 'export-2026-06-19.csv', 'text/csv')

      const link = createElementSpy.mock.results[createElementSpy.mock.results.length - 1]?.value
      expect(link?.download).toBe('export-2026-06-19.csv')
    })

    it('should handle filename with timestamp', () => {
      downloadExport('data', 'export_20260619_150000.json', 'application/json')

      const link = createElementSpy.mock.results[createElementSpy.mock.results.length - 1]?.value
      expect(link?.download).toBe('export_20260619_150000.json')
    })
  })

  describe('Integration scenarios', () => {
    it('should export CSV and download in workflow', () => {
      const containers = [
        {
          container_name: 'app-1',
          image: 'app:1.0',
          status: 'running',
          dso_awareness: { status: 'managed', managed_secrets: ['S1'], missing_mappings: [] },
        },
      ]

      const csvData = exportContainersToCSV(containers)
      expect(csvData).toBeTruthy()
      expect(csvData).toContain('app-1')

      const createElementSpy = vi.spyOn(document, 'createElement')
      const appendChildSpy = vi.spyOn(document.body, 'appendChild')
      const removeChildSpy = vi.spyOn(document.body, 'removeChild')

      downloadExport(csvData, 'containers.csv', 'text/csv')

      expect(createElementSpy).toHaveBeenCalledWith('a')
      expect(appendChildSpy).toHaveBeenCalled()
      expect(removeChildSpy).toHaveBeenCalled()

      vi.clearAllMocks()
    })

    it('should export JSON and download in workflow', () => {
      const containers = [
        {
          container_name: 'app-1',
          image: 'app:1.0',
          status: 'running',
          dso_awareness: { status: 'managed', managed_secrets: ['S1'], missing_mappings: [] },
        },
      ]

      const jsonData = exportContainersToJSON(containers)
      expect(jsonData).toBeTruthy()

      const parsed = JSON.parse(jsonData)
      expect(parsed[0].container_name).toBe('app-1')

      const createElementSpy = vi.spyOn(document, 'createElement')
      const appendChildSpy = vi.spyOn(document.body, 'appendChild')
      const removeChildSpy = vi.spyOn(document.body, 'removeChild')

      downloadExport(jsonData, 'containers.json', 'application/json')

      expect(createElementSpy).toHaveBeenCalledWith('a')
      expect(appendChildSpy).toHaveBeenCalled()
      expect(removeChildSpy).toHaveBeenCalled()

      vi.clearAllMocks()
    })
  })
})
