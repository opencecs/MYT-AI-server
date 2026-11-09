import { memo } from 'react'
import StorageSection from './StorageSection'
import McpSection from './McpSection'
import GeneralSection from './GeneralSection'
import AboutSection from './AboutSection'

function SettingsPage() {
  return (
    <div className="p-5 h-full flex flex-col">
      <div className="grid grid-cols-2 gap-4 items-stretch">
        {/* 左列：存储（高度大）+ MCP 紧跟其后平衡布局 */}
        <div className="space-y-4 flex flex-col">
          <StorageSection />
          {/* MCP 服务（新自建 MCPService，热启停；状态加载在组件内部） */}
          <McpSection />
        </div>
        <GeneralSection />
      </div>
      <div className="mt-auto pt-4">
        <AboutSection />
      </div>
    </div>
  )
}

export default memo(SettingsPage)
