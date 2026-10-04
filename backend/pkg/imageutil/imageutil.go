// Package imageutil 提供无副作用的图片处理工具：格式识别、尺寸读取、
// 等比缩放与缩略图生成。它不碰数据库、不读配置，所有参数显式传入，
// 调用方（上传链路）自行决定何时/如何使用。
//
// 依赖只有标准库（image/jpeg/png/gif 的解码与编码），不引入第三方库。
// WebP 标准库无法编解码，因此 Fit/Thumbnail 明确拒绝并提示保留原图。
package imageutil

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
)

// 处理上限：防止恶意大图把内存打爆。
//
//   - maxSourceBytes 限制输入字节数。Go 标准库解 JPEG 时按 SOF 里的
//     宽高直接分配像素缓冲，一个几 KB 的「解压炸弹」标称 100000x100000
//     就能吃掉数十 GB 内存，因此字节数必须设上限。
//   - maxDimension 限制解码后的宽高（同样针对解压炸弹）。
const (
	maxSourceBytes = 32 << 20 // 32MB
	maxDimension   = 20000    // 20000 像素

	defaultQuality   = 82  // JPEG 默认质量
	defaultThumbEdge = 320 // 缩略图默认最大边长
)

// 面向调用方的中文错误（不含敏感细节，不吐 .bin 调试输出）。
var (
	// ErrNotImage 输入不是可识别的图片格式。
	ErrNotImage = errors.New("无法识别的图片格式")
	// ErrTooLarge 输入图片字节数超过处理上限。
	ErrTooLarge = errors.New("图片文件过大，超过处理上限")
	// ErrWebPUnsupported WebP 标准库无法编解码，保留原图。
	ErrWebPUnsupported = errors.New("暂不支持 WebP 图片处理，已保留原图")
	// ErrDimensionsTooLarge 解码后宽高超过处理上限（疑似解压炸弹）。
	ErrDimensionsTooLarge = errors.New("图片尺寸过大，超过处理上限")
	// ErrBadBounds 缩放尺寸参数无效。
	ErrBadBounds = errors.New("缩放尺寸参数无效")
	// ErrDecode 图片数据损坏，标准库无法解码。
	ErrDecode = errors.New("图片解码失败，文件可能已损坏")
)

// Kind 识别图片格式。
//
// 返回 "jpeg"/"png"/"gif"/"webp"/"unknown"；ok=false 表示无法识别。
// WebP 按魔数可以识别（ok=true），但本包不能处理它，调用方应保留原图。
func Kind(data []byte) (format string, ok bool) {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "jpeg", true
	case len(data) >= 8 && bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "png", true
	case len(data) >= 6 &&
		(bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a"))):
		return "gif", true
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) &&
		bytes.Equal(data[8:12], []byte("WEBP")):
		return "webp", true
	default:
		return "unknown", false
	}
}

// Dimensions 返回图片宽高（像素）。
//
// 实现走标准库 image.DecodeConfig：它只解析文件头
// （JPEG SOF0/SOF2、PNG IHDR、GIF 逻辑屏幕描述符），不解码像素数据、
// 不分配全图缓冲，属于轻量级头部解析；相对全图 image.Decode 而言
// 内存与 CPU 开销都可以忽略。
func Dimensions(data []byte) (w, h int, err error) {
	if len(data) > maxSourceBytes {
		return 0, 0, ErrTooLarge
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, ErrNotImage
	}
	return cfg.Width, cfg.Height, nil
}

// Fit 把图片等比缩放到不超过 maxW x maxH 的尺寸（只缩不放）。
//
// format 判定与解码跟随输入字节本身（Kind + image.Decode）：
//   - JPEG 输出 JPEG，质量取 quality（1-100，<=0 用默认 82，>100 夹到 100）；
//   - PNG/GIF 保持原格式重编码（GIF 只取第一帧）。
//
// 已经不超过 maxW x maxH 时不重编码，直接返回原内容副本（避免无谓
// 二次压缩损失质量）。任何情况下都不修改入参 data，返回的是新字节。
func Fit(data []byte, maxW, maxH, quality int) (out []byte, err error) {
	if maxW <= 0 || maxH <= 0 {
		return nil, ErrBadBounds
	}
	if quality <= 0 {
		quality = defaultQuality
	}
	if quality > 100 {
		quality = 100
	}
	if len(data) > maxSourceBytes {
		return nil, ErrTooLarge
	}
	format, ok := Kind(data)
	if !ok {
		return nil, ErrNotImage
	}
	if format == "webp" {
		return nil, ErrWebPUnsupported
	}

	// 先用头部尺寸做解压炸弹防护：拒绝在分配全图缓冲之前，
	// 避免一个几十 KB 的图片把 GB 级内存吃掉。
	w, h, err := Dimensions(data)
	if err != nil {
		return nil, err
	}
	if w > maxDimension || h > maxDimension {
		return nil, ErrDimensionsTooLarge
	}

	// 只缩不放：已在上限之内时原样返回副本。
	if w <= maxW && h <= maxH {
		return append([]byte(nil), data...), nil
	}

	ratio := math.Min(float64(maxW)/float64(w), float64(maxH)/float64(h))
	nw := int(math.Round(float64(w) * ratio))
	nh := int(math.Round(float64(h) * ratio))
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrDecode
	}
	resized := resizeAreaAverage(img, nw, nh)

	var buf bytes.Buffer
	switch format {
	case "jpeg":
		if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: quality}); err != nil {
			return nil, ErrDecode
		}
	case "png":
		if err := png.Encode(&buf, resized); err != nil {
			return nil, ErrDecode
		}
	case "gif":
		// 只取第一帧重编码（image.Decode 对 GIF 本就只返回第一帧）。
		if err := gif.Encode(&buf, resized, nil); err != nil {
			return nil, ErrDecode
		}
	default:
		return nil, ErrNotImage
	}
	return buf.Bytes(), nil
}

// Thumbnail 是 Fit 的便捷包装：等比缩到 maxEdge 以内的小图。
// maxEdge<=0 时用默认 320。
func Thumbnail(data []byte, maxEdge int) (out []byte, err error) {
	if maxEdge <= 0 {
		maxEdge = defaultThumbEdge
	}
	return Fit(data, maxEdge, maxEdge, defaultQuality)
}

// resizeAreaAverage 用区域平均（box filter）把 src 缩放到 newW x newH。
//
// 只用于缩小（调用方保证 newW/newH 都不超过原尺寸），因此每个目标像素
// 的源区域非空。相比最近邻，区域平均在高倍缩小（如 4000px 缩到 320px）
// 时不会出现锯齿与摩尔纹，且无需任何第三方缩放库。
func resizeAreaAverage(src image.Image, newW, newH int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()

	// 统一成左上角为原点的 RGBA，方便下面按 stride 直接索引像素。
	// 注意 image.RGBA 的 Bounds 不保证从 (0,0) 开始，直接断言复用会有
	// 越界风险，因此只有 Min 为 (0,0) 时才免拷贝复用。
	var rgba *image.RGBA
	if r, ok := src.(*image.RGBA); ok && b.Min.X == 0 && b.Min.Y == 0 {
		rgba = r
	} else {
		rgba = image.NewRGBA(image.Rect(0, 0, sw, sh))
		draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	dstPix := dst.Pix
	dstStride := dst.Stride
	srcPix := rgba.Pix
	srcStride := rgba.Stride

	for y := 0; y < newH; y++ {
		y0 := y * sh / newH
		y1 := (y + 1) * sh / newH
		if y1 <= y0 {
			y1 = y0 + 1
		}
		if y1 > sh {
			y1 = sh
		}
		rows := y1 - y0
		for x := 0; x < newW; x++ {
			x0 := x * sw / newW
			x1 := (x + 1) * sw / newW
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if x1 > sw {
				x1 = sw
			}

			var rr, gg, bb, aa uint64
			for yy := y0; yy < y1; yy++ {
				row := srcPix[yy*srcStride+x0*4 : yy*srcStride+x1*4]
				for i := 0; i < len(row); i += 4 {
					rr += uint64(row[i])
					gg += uint64(row[i+1])
					bb += uint64(row[i+2])
					aa += uint64(row[i+3])
				}
			}
			n := uint64(rows) * uint64(x1-x0)
			o := y*dstStride + x*4
			dstPix[o+0] = uint8(rr / n)
			dstPix[o+1] = uint8(gg / n)
			dstPix[o+2] = uint8(bb / n)
			dstPix[o+3] = uint8(aa / n)
		}
	}
	return dst
}
