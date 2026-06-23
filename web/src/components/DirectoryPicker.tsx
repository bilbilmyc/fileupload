import { useState, useCallback, useEffect, useMemo } from 'react'
import { Modal, Tree, Spin, Input, Empty } from 'antd'
import { FolderOutlined, FolderOpenOutlined, SearchOutlined } from '@ant-design/icons'
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
    const res = await api.listFiles({parent: parentId})
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
  const [searchText, setSearchText] = useState('')

  // Load root directories
  useEffect(() => {
    if (!open) return
    setLoading(true)
    setSelectedKey('')
    setSelectedName('')
    setSearchText('')
    loadChildren('/').then((nodes) => {
      setTreeData(nodes)
      setLoading(false)
    })
  }, [open])

  const onLoadData = useCallback(async (node: DataNode): Promise<void> => {
    if (node.children || node.key === '__root__') return
    const children = await loadChildren(node.key as string)
    if (children.length > 0) {
      // @ts-ignore - mutate tree data
      node.children = children
      setTreeData([...treeData])
    }
  }, [treeData])

  const handleSelect = useCallback((keys: React.Key[], info: { node: DataNode }) => {
    if (keys.length > 0) {
      setSelectedKey(keys[0] as string)
      setSelectedName(info.node.title as string)
    }
  }, [])

  // Filter tree by search text
  const filterTree = useCallback((nodes: DataNode[], search: string): DataNode[] => {
    if (!search) return nodes
    const lower = search.toLowerCase()
    return nodes.reduce<DataNode[]>((acc, node) => {
      const titleStr = String(node.title || '')
      if (titleStr.toLowerCase().includes(lower)) {
        acc.push({ ...node })
      }
      const children = node.children ? filterTree(node.children, search) : undefined
      if (children && children.length > 0) {
        acc.push({ ...node, children })
      }
      return acc
    }, [])
  }, [])

  const filteredTree = useMemo(() => {
    return filterTree(treeData, searchText)
  }, [treeData, searchText, filterTree])

  // Add root option at top
  const displayTree: DataNode[] = useMemo(() => {
    const rootNode: DataNode = {
      key: '',
      title: '根目录',
      icon: <FolderOpenOutlined />,
      isLeaf: true,
    }
    return [rootNode, ...filteredTree]
  }, [filteredTree])

  const handleConfirm = () => {
    if (selectedKey === undefined) {
      return // root is valid (key = '')
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
      width={500}
    >
      <div className="mb-3">
        <Input
          size="small"
          prefix={<SearchOutlined className="text-gray-400" />}
          placeholder="搜索目录..."
          value={searchText}
          onChange={(e) => setSearchText(e.target.value)}
          allowClear
          autoFocus
        />
      </div>
      {selectedName && (
        <div className="mb-2 text-xs text-blue-600 bg-blue-50 px-2.5 py-1.5 rounded-md border border-blue-100">
          已选择: <strong>{selectedName}</strong>
        </div>
      )}
      {loading ? (
        <div className="flex justify-center py-8"><Spin /></div>
      ) : filteredTree.length === 0 && searchText ? (
        <Empty description="无匹配目录" className="py-8" />
      ) : (
        <div className="max-h-72 overflow-auto border border-gray-100 rounded-md p-1">
          <Tree
            treeData={displayTree}
            loadData={onLoadData as any}
            onSelect={handleSelect}
            showIcon
            defaultExpandAll={false}
            selectedKeys={selectedKey ? [selectedKey] : []}
          />
        </div>
      )}
    </Modal>
  )
}
