package imageutil

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

// makePNG 生成一张 w x h 的纯色 PNG，用于测试缩放。
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("编码 PNG 失败: %v", err)
	}
	return buf.Bytes()
}

func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("编码 JPEG 失败: %v", err)
	}
	return buf.Bytes()
}

func makeGIF(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{
		color.RGBA{0, 0, 0, 255}, color.RGBA{255, 255, 255, 255},
	})
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatalf("编码 GIF 失败: %v", err)
	}
	return buf.Bytes()
}

func TestKind(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
		ok   bool
	}{
		{"PNG", makePNG(t, 4, 4), "png", true},
		{"JPEG", makeJPEG(t, 4, 4), "jpeg", true},
		{"GIF", makeGIF(t, 4, 4), "gif", true},
		{"WebP", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), "webp", true},
		{"空数据", nil, "unknown", false},
		{"纯文本", []byte("hello world"), "unknown", false},
		{"截断的PNG魔数", []byte("\x89PN"), "unknown", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Kind(c.data)
			if got != c.want || ok != c.ok {
				t.Errorf("Kind() = (%q, %v)，想要 (%q, %v)", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestDimensions(t *testing.T) {
	data := makePNG(t, 120, 80)
	w, h, err := Dimensions(data)
	if err != nil {
		t.Fatalf("Dimensions 失败: %v", err)
	}
	if w != 120 || h != 80 {
		t.Errorf("Dimensions() = (%d, %d)，想要 (120, 80)", w, h)
	}
}

func TestDimensionsRejectsNonImage(t *testing.T) {
	if _, _, err := Dimensions([]byte("not an image")); err == nil {
		t.Error("非图片输入应返回错误")
	}
}

// TestFitShrinksButNeverEnlarges 是 Fit 最核心的契约：只缩不放。
// 小图必须原样返回（内容与入参逐字节相同），否则列表里的小图标会被
// 反复有损压缩，越存越糊。
func TestFitShrinksButNeverEnlarges(t *testing.T) {
	small := makeJPEG(t, 100, 100)
	out, err := Fit(small, 2560, 2560, 0)
	if err != nil {
		t.Fatalf("Fit 小图失败: %v", err)
	}
	if !bytes.Equal(out, small) {
		t.Error("尺寸已在上限内的图应原样返回，不该重编码")
	}

	big := makeJPEG(t, 4000, 3000)
	out, err = Fit(big, 2560, 2560, 0)
	if err != nil {
		t.Fatalf("Fit 大图失败: %v", err)
	}
	w, h, err := Dimensions(out)
	if err != nil {
		t.Fatalf("读取缩放结果尺寸失败: %v", err)
	}
	if w != 2560 || h != 1920 {
		t.Errorf("等比缩放结果 = (%d, %d)，想要 (2560, 1920)", w, h)
	}
	if bytes.Equal(out, big) {
		t.Error("超限大图应被重编码，不该原样返回")
	}
}

// TestFitDoesNotMutateInput 确认入参不被改写：调用方上传链路里
// 原图数据还要复用（生成缩略图），被改就会静默出错。
func TestFitDoesNotMutateInput(t *testing.T) {
	big := makeJPEG(t, 2000, 2000)
	before := append([]byte(nil), big...)
	if _, err := Fit(big, 320, 320, 0); err != nil {
		t.Fatalf("Fit 失败: %v", err)
	}
	if !bytes.Equal(big, before) {
		t.Error("Fit 修改了入参数据，调用方复用原图时会出错")
	}
}

func TestFitKeepsAspectRatioOnPortrait(t *testing.T) {
	// 竖图：长边是高度，应按高度限制缩放，宽度跟着缩。
	data := makeJPEG(t, 1000, 4000)
	out, err := Fit(data, 320, 320, 0)
	if err != nil {
		t.Fatalf("Fit 竖图失败: %v", err)
	}
	w, h, err := Dimensions(out)
	if err != nil {
		t.Fatal(err)
	}
	if h != 320 || w != 80 {
		t.Errorf("竖图缩放结果 = (%d, %d)，想要 (80, 320)", w, h)
	}
}

func TestThumbnail(t *testing.T) {
	data := makePNG(t, 1000, 500)
	out, err := Thumbnail(data, 320)
	if err != nil {
		t.Fatalf("Thumbnail 失败: %v", err)
	}
	w, h, err := Dimensions(out)
	if err != nil {
		t.Fatal(err)
	}
	if w != 320 || h != 160 {
		t.Errorf("缩略图尺寸 = (%d, %d)，想要 (320, 160)", w, h)
	}
}

func TestThumbnailDefaultEdge(t *testing.T) {
	// maxEdge<=0 应取默认 320，而不是报错或返回 0 尺寸。
	data := makePNG(t, 800, 800)
	out, err := Thumbnail(data, 0)
	if err != nil {
		t.Fatalf("Thumbnail(0) 失败: %v", err)
	}
	w, _, err := Dimensions(out)
	if err != nil {
		t.Fatal(err)
	}
	if w != defaultThumbEdge {
		t.Errorf("默认边长 = %d，想要 %d", w, defaultThumbEdge)
	}
}

func TestFitRejectsBadBounds(t *testing.T) {
	if _, err := Fit(makePNG(t, 10, 10), 0, 100, 0); err == nil {
		t.Error("maxW<=0 应返回错误")
	}
	if _, err := Fit(makePNG(t, 10, 10), 100, -1, 0); err == nil {
		t.Error("maxH<=0 应返回错误")
	}
}

func TestFitRejectsWebP(t *testing.T) {
	// WebP 标准库无法编解码，必须明确拒绝并让调用方保留原图，
	// 不能返回一个半成品的图。
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBP"), make([]byte, 64)...)
	_, err := Fit(webp, 320, 320, 0)
	if err == nil {
		t.Fatal("WebP 应被拒绝")
	}
	if err != ErrWebPUnsupported {
		t.Errorf("错误 = %v，想要 ErrWebPUnsupported", err)
	}
}

func TestFitRejectsGarbage(t *testing.T) {
	if _, err := Fit([]byte("this is not an image at all"), 320, 320, 0); err == nil {
		t.Error("非图片数据应返回错误")
	}
}

// TestFitRejectsOversizedInput 防磁盘/内存被打满：超限必须在解码前拦下。
func TestFitRejectsOversizedInput(t *testing.T) {
	var huge [maxSourceBytes + 1]byte
	// 用合法 JPEG 魔数开头，确保是因为体积而不是格式被拒。
	huge[0], huge[1], huge[2] = 0xFF, 0xD8, 0xFF
	if _, err := Fit(huge[:], 320, 320, 0); err == nil {
		t.Error("超过体积上限应返回错误")
	}
}

func TestResizeAreaAverageOutputSize(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 64, 48))
	dst := resizeAreaAverage(src, 16, 12)
	if b := dst.Bounds(); b.Dx() != 16 || b.Dy() != 12 {
		t.Errorf("缩放结果尺寸 = %dx%d，想要 16x12", b.Dx(), b.Dy())
	}
}

// TestResizeAreaAveragePreservesAverageColor 确认区域平均真的在取均值，
// 而不是 nearest-neighbor 采样（否则高倍缩小会出现锯齿和色偏）。
//
// 预期值直接按源区域算，不写死数字——边界是否对齐取决于缩放比，
// 写死数字会像第一版那样把「测试自身选错像素」当成产品 bug。
func TestResizeAreaAveragePreservesAverageColor(t *testing.T) {
	const sw, sh = 32, 30
	src := image.NewRGBA(image.Rect(0, 0, sw, sh))
	// 左半黑、右半白。
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			if x < sw/2 {
				src.Set(x, y, color.RGBA{0, 0, 0, 255})
			} else {
				src.Set(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
	}

	// 目标宽度取 7：32/7 不是整数，必有一个目标像素的源区间跨过 x=16
	// 的黑白分界，才能真正验证「取的是均值」而非「取某一点的采样」。
	const newW, newH = 7, 7
	dst := resizeAreaAverage(src, newW, newH)

	for ty := 0; ty < newH; ty++ {
		y0, y1 := ty*sh/newH, (ty+1)*sh/newH
		if y1 <= y0 {
			y1 = y0 + 1
		}
		if y1 > sh {
			y1 = sh
		}
		for tx := 0; tx < newW; tx++ {
			x0, x1 := tx*sw/newW, (tx+1)*sw/newW
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if x1 > sw {
				x1 = sw
			}
			// 源区域在黑白分界两侧的像素个数
			black := (sw/2 - x0)
			if black < 0 {
				black = 0
			}
			if black > x1-x0 {
				black = x1 - x0
			}
			white := (x1 - x0) - black
			// 均值换算成 16 位（RGBA() 的返回域）
			want := (white * 0xffff) / (x1 - x0)

			got := color.GrayModel.Convert(dst.At(tx, ty)).(color.Gray).Y
			// 允许 1/255 的整除误差
			if diff := int(got) - int(want>>8); diff < -1 || diff > 1 {
				t.Errorf("像素(%d,%d) = %d，期望约 %d（源区间 [%d,%d)，黑%d/白%d）",
					tx, ty, got, want>>8, x0, x1, black, white)
			}
		}
	}
}
