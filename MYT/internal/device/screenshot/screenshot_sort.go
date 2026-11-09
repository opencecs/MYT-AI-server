package screenshot

import (
	"sort"

	"github.com/moyunteng/myt/internal/config"
)

// SortTargets 按设备 + 坑位号排序截图轮询目标，与前端截图模式多选展示顺序对齐：
//   - local 本地模式：按设备 IP 数值序，同 IP 内按坑位号升序
//   - public 公网模式：按设备 ID 字符串序，同 ID 内按坑位号升序
//
// 同设备的坑位相邻（分组排序），决定 worker 任务派发顺序。
func SortTargets(targets []ContainerTarget, deployMode string) {
	cmp := deviceThenSlotComparator(deployMode)
	sort.SliceStable(targets, func(i, j int) bool {
		return cmp(targets[i], targets[j]) < 0
	})
}

// deviceThenSlotComparator 返回 (设备, 坑位号) 字典序比较器。
func deviceThenSlotComparator(deployMode string) func(a, b ContainerTarget) int {
	return func(a, b ContainerTarget) int {
		var cmp int
		if deployMode == "public" {
			switch {
			case a.DeviceID < b.DeviceID:
				cmp = -1
			case a.DeviceID > b.DeviceID:
				cmp = 1
			}
		} else {
			cmp = config.CompareIP(a.DeviceIP, b.DeviceIP)
		}
		if cmp != 0 {
			return cmp
		}
		return a.IndexNum - b.IndexNum
	}
}
