import { useState } from 'react'

export function useInlineEdit(onSave: (id: string, value: string) => Promise<void>) {
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editValue, setEditValue] = useState('')

  const startEdit = (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    setEditingId(id)
    setEditValue('')
  }
  const cancelEdit = () => { setEditingId(null); setEditValue('') }
  const saveNote = async (customValue?: string) => {
    if (!editingId) return
    const t = (customValue ?? editValue).trim()
    await onSave(editingId, t)
    setEditingId(null); setEditValue('')
  }

  return { editingId, editValue, setEditValue, startEdit, cancelEdit, saveNote }
}
