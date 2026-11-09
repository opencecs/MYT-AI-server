#!/usr/bin/env bash
# tebox-api 多实例基座。guest 用 CreateInstance，imgs 放 user-images/。
# 端口：gRPC 50051 | video 9901 | audio 9904 | control 9905
# 来源：上游 start-tebox-hw.sh 原样入库（安装流程不改脚本逻辑——容器已存在
# 也按脚本行为 rm -f 删掉重建；由 maintenance 包 go:embed 上传到设备执行）。
set -euo pipefail
IMAGE="${IMAGE:-tebox:1.0.0}"
NAME="${NAME:-tebox}"
docker rm -f "$NAME" tebox-hw 2>/dev/null || true
mkdir -p /mmc/tebox/{data,user-images,instances}
docker run -d --name "$NAME" --restart unless-stopped --network host \
  --device /dev/kvm --device /dev/mali0 --device /dev/dri \
  --device /dev/dma_heap/system --device /dev/dma_heap/system-uncached \
  --device /dev/video0 --device /dev/video1 \
  -e TEBOX_MALI=0 \
  -e TEBOX_GRPC_ADDR=0.0.0.0:50051 \
  -e TEBOX_CAST_ADDR=0.0.0.0:9901 \
  -e TEBOX_AUDIO_ADDR=0.0.0.0:9904 \
  -e TEBOX_CONTROL_ADDR=0.0.0.0:9905 \
  -v /mmc/tebox/data:/data \
  -v /mmc/tebox/user-images:/mmc/tebox/user-images:ro \
  -v /mmc/tebox/instances:/mmc/tebox/instances \
  "$IMAGE"
echo "base up: gRPC :50051  cast :9901  image=$IMAGE"
