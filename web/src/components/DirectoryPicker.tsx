import { useState, useCallback, useEffect } from 'react'
import { Modal, Tree, Spin, message } from 'antd'
import { FolderOutlined, FolderOpenOutlined } from '@ant-design/icons'
import type { DataNode } from 'antd/es/tree'
import * as api from '../api/client'
import type { FileItem } from '../api/client'

interface DirectoryPickerProps {
  open: boolean
  title: string
  onCancel: () => void
  onConfirm: (dirId: string, dirName: string) => void
}

async function loadChildren(parentId: string): Promise<DataNode[]> {
  try {
    const res = await api.listFiles(parentId)
    const dirs = (res.children || []).filter((c: FileItem) => c.is_dir)
    return dirs.map((d: FileItem) => ({
      key: d.file_id,
      title: d.name,
      icon: <FolderOutlined />,
      isLeaf: false,
    }))
  } catch {
    return []
  }
}

export default function DirectoryPicker({
  open,
  title,
  onCancel,
  onConfirm,
}: DirectoryPickerProps) {
  const [treeData, setTreeData] = useState<DataNode[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedKey, setSelectedKey] = useState<string>('')
  const [selectedName, setSelectedName] = useState<string>('')

  // Load root directories
  useEffect(() => {
    if (!open) return
    setLoading(true)
    setSelectedKey('')
    setSelectedName('')
    loadChildren('/').then((nodes) => {
      // Add "root" option
      const rootNode: DataNode = {
        key: '',
        title: '根目录',
        icon: <FolderOpenOutlined />,
        isLeaf: true,
      }
      setTreeData([rootNode, ...nodes])
      setLoading(false)
    })
  }, [open])

  const onLoadData = useCallback(async (node: DataNode): Promise<void> => {
    // Don't load if already loaded or it's the root pseudo-node
    if (node.children || node.key === '') return
    const children = await loadChildren(node.key as string)
    // @ts-ignore - mutate tree data
    node.children = children.length > 0 ? children : [{ key: `${node.key}-empty`, title: '(空目录)', isLeaf: true, disabled: true }]
    setTreeData([...treeData])
  }, [treeData])

  const handleSelect = useCallback((keys: React.Key[], info: { node: DataNode }) => {
    if (keys.length > 0) {
      setSelectedKey(keys[0] as string)
      setSelectedName(info.node.title as string)
    }
  }, [])

  const handleConfirm = () => {
    if (!selectedKey && selectedKey !== '') {
      message.warning('请选择一个目录')
      return
    }
    onConfirm(selectedKey, selectedName)
  }

  return (
    <Modal
      title={title}
      open={open}
      onOk={handleConfirm}
      onCancel={onCancel}
      okText="确认"
      cancelText="取消"
      width={400}
    >
      {loading ? (
        <div className="flex justify-center py-8"><Spin /></div>
      ) : (
        <div className="max-h-80 overflow-auto">
          {selectedName && selectedKey !== '' && (
            <div className="mb-2 text-xs text-blue-600 bg-blue-50 px-2 py-1 rounded">
              已选择: {selectedName}
            </div>
          )}
          <Tree
            treeData={treeData}
            loadData={onLoadData as any}
            onSelect={handleSelect}
            showIcon
            defaultExpandAll={false}
          />
        </div>
      )}
    </Modal>
  )
}
