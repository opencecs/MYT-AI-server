package iscsitarget

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
)

// ThinBackend 动态扩展（thin provisioning）的磁盘映像文件
type ThinBackend struct {
	file     *os.File
	size     int64
	blkSize  int64
	dataOff  int64
	blkCount int64
	mu       sync.Mutex

	// bitmap 内存缓存：启动时读入，避免每次写都做 file.ReadAt 查 allocation 状态。
	// 新分配块的 bitmap 字节在 Write 内即时 WriteAt 落文件（与数据同走 page cache，
	// 由 Sync 统一持久化）——保证 target 进程崩溃（page cache 不丢）后重开映像时
	// bitmap 与数据一致，不会把已写块误判为未分配而读出全零
	bitmap []byte
}

const thinBlockSize = 1024 * 1024 // 1MB per block

func NewThinBackend(path string, sizeGB int64) (*ThinBackend, error) {
	size := sizeGB * 1024 * 1024 * 1024
	blkCount := (size + thinBlockSize - 1) / thinBlockSize

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("create thin image file: %w", err)
	}

	metaSize := 8 + blkCount + 1
	metaSize = (metaSize + 4095) & ^4095

	b := &ThinBackend{
		file:     f,
		size:     size,
		blkSize:  thinBlockSize,
		dataOff:  metaSize,
		blkCount: blkCount,
	}

	var sizeBuf [8]byte
	binary.LittleEndian.PutUint64(sizeBuf[:], uint64(size))
	if _, err := f.WriteAt(sizeBuf[:], 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("write thin metadata: %w", err)
	}

	// 初始化 bitmap（全零=全未分配），新建文件不需要读磁盘
	b.bitmap = make([]byte, blkCount+1)

	return b, nil
}

func OpenThinBackend(path string) (*ThinBackend, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open thin image file: %w", err)
	}

	var sizeBuf [8]byte
	if _, err := f.ReadAt(sizeBuf[:], 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("read thin metadata: %w", err)
	}
	size := int64(binary.LittleEndian.Uint64(sizeBuf[:]))
	blkCount := (size + thinBlockSize - 1) / thinBlockSize

	metaSize := 8 + blkCount + 1
	metaSize = (metaSize + 4095) & ^4095

	// 读入 bitmap 到内存缓存
	bitmap := make([]byte, blkCount+1)
	if _, err := f.ReadAt(bitmap[:blkCount], 8); err != nil && err.Error() != "EOF" {
		// 新建/空文件，bitmap 全零
	}

	return &ThinBackend{
		file:      f,
		size:      size,
		blkSize:   thinBlockSize,
		dataOff:   metaSize,
		blkCount:  blkCount,
		bitmap:    bitmap,
	}, nil
}

func (b *ThinBackend) isAllocated(blk int64) bool {
	return b.bitmap[blk] == 1
}

func (b *ThinBackend) blockFileOffset(blk int64) int64 {
	return b.dataOff + blk*b.blkSize
}

func (b *ThinBackend) Read(at int64, buf []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	offset := at
	remaining := len(buf)
	bufOff := 0

	// 预分配零缓冲区，避免大磁盘未分配块逐字节填零
	zeroBuf := make([]byte, b.blkSize)

	for remaining > 0 {
		blk := offset / b.blkSize
		blkOff := offset % b.blkSize
		toRead := int64(remaining)
		if toRead > b.blkSize-blkOff {
			toRead = b.blkSize - blkOff
		}

		if blk >= b.blkCount {
			return fmt.Errorf("read beyond virtual size")
		}

		if !b.isAllocated(blk) {
			// 用 copy 替代逐字节填零，大磁盘性能提升显著
			copy(buf[bufOff:bufOff+int(toRead)], zeroBuf[:toRead])
		} else {
			_, err := b.file.ReadAt(buf[bufOff:bufOff+int(toRead)], b.blockFileOffset(blk)+blkOff)
			if err != nil {
				return err
			}
		}

		offset += toRead
		bufOff += int(toRead)
		remaining -= int(toRead)
	}

	return nil
}

func (b *ThinBackend) Write(at int64, buf []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	offset := at
	remaining := len(buf)
	bufOff := 0
	firstNew, lastNew := int64(-1), int64(-1)

	for remaining > 0 {
		blk := offset / b.blkSize
		blkOff := offset % b.blkSize
		toWrite := int64(remaining)
		if toWrite > b.blkSize-blkOff {
			toWrite = b.blkSize - blkOff
		}

		if blk >= b.blkCount {
			return fmt.Errorf("write beyond virtual size")
		}

		if !b.isAllocated(blk) {
			b.bitmap[blk] = 1
			if firstNew < 0 {
				firstNew = blk
			}
			lastNew = blk
		}

		_, err := b.file.WriteAt(buf[bufOff:bufOff+int(toWrite)], b.blockFileOffset(blk)+blkOff)
		if err != nil {
			return err
		}

		offset += toWrite
		bufOff += int(toWrite)
		remaining -= int(toWrite)
	}

	// 新分配块的 bitmap 随写立即落盘（不 fsync）：数据先写、标记后写，两者同走
	// page cache，由 Sync（SYNCHRONIZE CACHE / FUA）统一持久化。这样 target 进程
	// 崩溃（page cache 不丢）时重开映像，bitmap 与数据都在；系统断电时未 Sync 的
	// 写整体表现为丢失（与真实磁盘 write cache 断电语义一致），不会出现
	//「数据在而标记丢」之外的不一致。bitmap 写失败视为本次写失败——无标记的
	// 数据页读零，自洽
	if firstNew >= 0 {
		if _, err := b.file.WriteAt(b.bitmap[firstNew:lastNew+1], 8+firstNew); err != nil {
			return fmt.Errorf("persist bitmap: %w", err)
		}
	}
	return nil
}

// Sync 持久化数据与 bitmap 到物理介质。bitmap 已随 Write 即时写入文件，
// 这里一次 file.Sync 把所有脏页（数据 + bitmap）刷下即可
func (b *ThinBackend) Sync() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.file.Sync()
}

func (b *ThinBackend) Size() int64  { return b.size }
func (b *ThinBackend) Close() error { return b.file.Close() }
