/**
 * 截图卡片换图防闪 hook（`ScreenshotCard` 专用）。
 *
 * ## useStableImageSrc —— 换图不闪
 *
 * 现象：轮询拿到新图后直接给 `<img>` 换 `src`，浏览器会**先丢弃旧帧**再解码新图，
 * 解码完成前该区域画不出内容 → 露出父容器底色（`bg-gray-900` 深色底），观感就是
 * 「每次截图变化闪一下」。CDP screencast 实测（V4 720x1600 PNG，~277KB）可见连续
 * 3 帧整块底色；同一机制 V3 也存在（V3 同样直接改 src），只是 V3 走 9082
 * `?task=snap&level=1` 返回的是小体积 JPEG、解码窗口短到看不出来。
 *
 * 修法：**预加载 + decode 成功后才替换**。新 data URL 先喂给游离的 `new Image()`，
 * `decode()` 成功（已解码进内存缓存）后才 setState 换到真实 `<img>` 的 src ——
 * 此时浏览器能立即绘制新帧，不产生空白窗口。解码失败（异常数据）则保留旧图。
 *
 * 为什么用 `decode()` 而不是 `onload`：`onload` 只保证字节到齐，解码仍可能延后
 * （`decoding="async"` 会把解码放到别的线程），换 src 后照样有空白帧。`decode()`
 * 的 Promise 在**解码完成**时才 resolve，正是我们要的时点。
 *
 * ## 尺寸处理：容器比例 = 本页主流截图比例 + object-cover（不在此文件）
 *
 * 截图区容器比例由宿主统一计算下发（`screenshotDetail/useDominantAspect.ts`：按卡片
 * `<img>` onLoad 的 `naturalWidth/naturalHeight` 统计本页**主流**比例，取众数），整页所有
 * 卡片共用同一比例 → 卡片等高；图片一律 `object-cover` 填满容器：比例一致时零黑边零裁切，
 * 个别分辨率不同的 ROM 极轻微裁切。
 *
 * 历史：曾固定 `aspect-[9/16]` + `object-contain`，V4(tobox) 的 9:20 截图在 9:16 容器里
 * 会左右各留一条黑边（用户反馈不可接受）；也曾用 `useNaturalAspect` 按单图真实比例改
 * 容器高度，但各 ROM 分辨率不同 → 卡片墙高度参差不齐，已弃用并删除。
 */

import { useEffect, useRef, useState } from 'react'

/**
 * 返回「可以安全显示」的图片地址：新地址预加载并解码成功后才切换，失败保留旧值。
 *
 * @param src 目标地址（data URL）；null 表示清空（切模式/关机等）
 */
export function useStableImageSrc(src: string | null): string | null {
  const [shown, setShown] = useState<string | null>(src)
  // 用 ref 读当前显示值：避免把 shown 放进 effect 依赖（那会让每次切换都重跑一次
  // effect 并新建 Image 对象；同一 src 的重复预加载纯属浪费）
  const shownRef = useRef<string | null>(shown)
  shownRef.current = shown

  useEffect(() => {
    if (!src) {
      setShown(null)
      return
    }
    if (src === shownRef.current) return
    let cancelled = false
    const pre = new Image()
    pre.src = src
    const commit = () => { if (!cancelled) setShown(src) }
    if (typeof pre.decode === 'function') {
      pre.decode().then(commit).catch(() => { /* 解码失败：保留旧图，等下一轮推送 */ })
    } else {
      // 老内核兜底（WebView2 全部支持 decode，此处仅防御）
      pre.onload = commit
      pre.onerror = () => {}
    }
    return () => { cancelled = true }
  }, [src])

  return shown
}
