import { useTranslation } from 'react-i18next'
import { prefixColor } from './VpcDomainShared'

/** 前缀彩色徽标（domain/full/keyword/regexp/ip 五色；空前缀=无前缀/未知前缀原样行，显示「原样」灰字） */
export default function PrefixBadge({ prefix }: { prefix: string }) {
  const { t } = useTranslation()
  if (!prefix) {
    return (
      <span className="px-1.5 py-0.5 rounded font-medium bg-gray-100 text-gray-400">
        {t('vpc:domainShared.rawPrefix')}
      </span>
    )
  }
  return <span className={`px-1.5 py-0.5 rounded font-medium ${prefixColor(prefix)}`}>{prefix.replace(':', '')}</span>
}
