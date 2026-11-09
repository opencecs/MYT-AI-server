import { useTranslation } from 'react-i18next'

// 使用说明：文案对齐 pc-android-client streamGuideHtml（转发流 / P2P / PC 摄像头三段）
export default function StreamGuideTab() {
  const { t } = useTranslation()
  return (
    <div className="h-full overflow-y-auto p-5 text-xs text-gray-700 leading-relaxed">
      <h3 className="text-sm font-semibold text-gray-800 mb-3 px-3 py-1.5 bg-blue-50 border-l-4 border-blue-400 rounded-r">
        {t('stream:guide.title')}
      </h3>
      <div className="space-y-4">
        <section>
          <h4 className="font-medium text-gray-800 mb-1">{t('stream:guide.forwardTitle')}</h4>
          <ol className="list-decimal pl-5 space-y-1 text-gray-600">
            <li>{t('stream:guide.forwardSteps.1')}</li>
            <li>{t('stream:guide.forwardSteps.2')}</li>
            <li>{t('stream:guide.forwardSteps.3')}</li>
            <li>{t('stream:guide.forwardSteps.4')}</li>
            <li>{t('stream:guide.forwardSteps.5')}</li>
            <li>{t('stream:guide.forwardSteps.6')}</li>
          </ol>
        </section>
        <section>
          <h4 className="font-medium text-gray-800 mb-1">{t('stream:guide.p2pTitle')}</h4>
          <ol className="list-decimal pl-5 space-y-1 text-gray-600">
            <li>{t('stream:guide.p2pSteps.1')}</li>
            <li>{t('stream:guide.p2pSteps.2')}</li>
            <li>{t('stream:guide.p2pSteps.3')}</li>
            <li>{t('stream:guide.p2pSteps.4')}</li>
            <li>{t('stream:guide.p2pSteps.5')}</li>
          </ol>
        </section>
        <section>
          <h4 className="font-medium text-gray-800 mb-1">{t('stream:guide.cameraTitle')}</h4>
          <ol className="list-decimal pl-5 space-y-1 text-gray-600">
            <li>{t('stream:guide.cameraSteps.1')}</li>
            <li>{t('stream:guide.cameraSteps.2')}</li>
            <li>{t('stream:guide.cameraSteps.3')}</li>
            <li>{t('stream:guide.cameraSteps.4')}</li>
          </ol>
        </section>
      </div>
    </div>
  )
}
