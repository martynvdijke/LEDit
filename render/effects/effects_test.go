package effects

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

var nativeEffectNames = []string{"fire", "gradient", "noise", "plasma", "scan", "sparkle"}

func testParams() Params {
	return Params{
		Palette:   DefaultPalette(),
		Speed:     50,
		Intensity: 50,
		Density:   50,
		Direction: DirectionLeft,
		Seed:      7,
	}
}

func altPalette() Palette {
	return PaletteFromRGB([3]uint8{0x10, 0x00, 0x20}, [3]uint8{0xff, 0x00, 0x80}, [3]uint8{0xff, 0xf0, 0x00})
}

func frameHash(img image.Image) string {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:8])
}

func renderBytes(name string, w, h int, p Params, elapsed time.Duration) []byte {
	r, ok := NewRendererFor(name)
	if !ok {
		return nil
	}
	img := r.Frame(w, h, p, elapsed)
	if img == nil {
		return nil
	}
	out := make([]byte, len(img.Pix))
	copy(out, img.Pix)
	return out
}

func TestRegistryContainsBuiltinsInStableOrder(t *testing.T) {
	names := Names()
	if len(names) != len(nativeEffectNames) {
		t.Fatalf("Names() = %v, want the %d native built-ins", names, len(nativeEffectNames))
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("Names() = %v, want sorted order", names)
	}
	for i, want := range nativeEffectNames {
		if names[i] != want {
			t.Fatalf("Names()[%d] = %q, want %q (full: %v)", i, names[i], want, names)
		}
	}
	for _, name := range nativeEffectNames {
		desc, eff, ok := Lookup(name)
		if !ok || eff == nil {
			t.Fatalf("Lookup(%q) failed", name)
		}
		if desc.Name != name || eff.Name() != name {
			t.Fatalf("descriptor/effect name mismatch for %q: %q %q", name, desc.Name, eff.Name())
		}
		if len(desc.Bounds.Directions) != len(ValidDirections()) {
			t.Fatalf("effect %q declares directions %v", name, desc.Bounds.Directions)
		}
	}
	if _, _, ok := Lookup("does-not-exist"); ok {
		t.Fatal("Lookup of an unknown effect succeeded")
	}
	descs := Descriptors()
	if len(descs) != len(names) || descs[0].Name != names[0] {
		t.Fatalf("Descriptors() = %v, want %v", descs, names)
	}
}

func TestValidateReportsFieldErrors(t *testing.T) {
	if err := Validate("plasma", testParams()); err != nil {
		t.Fatalf("valid params rejected: %v", err)
	}
	if err := Validate("nope", testParams()); err == nil || !strings.Contains(err.Error(), "unknown effect") {
		t.Fatalf("unknown effect error = %v", err)
	}
	cases := []struct {
		name  string
		mut   func(p *Params)
		field string
	}{
		{"speed", func(p *Params) { p.Speed = 101 }, "speed"},
		{"negative intensity", func(p *Params) { p.Intensity = -1 }, "intensity"},
		{"density", func(p *Params) { p.Density = 1000 }, "density"},
		{"direction", func(p *Params) { p.Direction = "sideways" }, "direction"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := testParams()
			tc.mut(&p)
			err := Validate("plasma", p)
			if err == nil {
				t.Fatal("out-of-range params accepted")
			}
			fields, ok := FieldErrorsOf(err)
			if !ok {
				t.Fatalf("error %v carries no field information", err)
			}
			if len(fields) != 1 || fields[0].Field != tc.field {
				t.Fatalf("fields = %v, want a single %q error", fields, tc.field)
			}
			if strings.TrimSpace(fields[0].Message) == "" {
				t.Fatal("field error has an empty message")
			}
		})
	}
	// An empty direction means "unset" and is accepted.
	p := testParams()
	p.Direction = ""
	if err := Validate("plasma", p); err != nil {
		t.Fatalf("empty direction rejected: %v", err)
	}
}

func TestFieldMapSummarizesErrors(t *testing.T) {
	p := testParams()
	p.Speed = 200
	p.Direction = "laterally"
	fields := FieldMap(Validate("plasma", p))
	if len(fields) != 2 {
		t.Fatalf("FieldMap = %v, want two entries", fields)
	}
	if fields["speed"] == "" || fields["direction"] == "" {
		t.Fatalf("FieldMap = %v, want speed and direction", fields)
	}
	if got := FieldMap(Validate("nope", p)); got["effect"] == "" {
		t.Fatalf("FieldMap for unknown effect = %v", got)
	}
	if got := FieldMap(nil); got != nil {
		t.Fatalf("FieldMap(nil) = %v, want nil", got)
	}
}

func TestParamsJSONRoundTrip(t *testing.T) {
	p := testParams()
	p.Seed = -99
	p.Direction = DirectionUp
	p.Palette = altPalette()
	data, err := MarshalParams(p)
	if err != nil {
		t.Fatalf("MarshalParams: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("serialized params are not JSON: %v", err)
	}
	if doc["v"] != float64(ParamsSchemaVersion) {
		t.Fatalf("serialized version = %v, want %d", doc["v"], ParamsSchemaVersion)
	}
	got, err := UnmarshalParams(data)
	if err != nil {
		t.Fatalf("UnmarshalParams: %v", err)
	}
	if got != p {
		t.Fatalf("round trip = %+v, want %+v", got, p)
	}

	// Behaviour-only documents omit the palette and round-trip to a zero palette.
	stripped := p.StripPalette()
	data, err = MarshalParams(stripped)
	if err != nil {
		t.Fatalf("MarshalParams(stripped): %v", err)
	}
	got, err = UnmarshalParams(data)
	if err != nil {
		t.Fatalf("UnmarshalParams(stripped): %v", err)
	}
	if got != stripped {
		t.Fatalf("stripped round trip = %+v, want %+v", got, stripped)
	}

	// Unknown fields are ignored and newer schema versions are rejected.
	got, err = UnmarshalParams([]byte(`{"v":1,"speed":10,"future_field":{"a":1}}`))
	if err != nil {
		t.Fatalf("UnmarshalParams with unknown field: %v", err)
	}
	if got.Speed != 10 {
		t.Fatalf("speed = %v, want 10", got.Speed)
	}
	if _, err := UnmarshalParams([]byte(`{"v":99,"speed":10}`)); err == nil {
		t.Fatal("newer schema version accepted")
	}
	if empty, err := UnmarshalParams([]byte("  ")); err != nil || empty != (Params{}) {
		t.Fatalf("UnmarshalParams(empty) = %+v, %v", empty, err)
	}
}

func TestClampAndDefaults(t *testing.T) {
	p := testParams()
	p.Speed = 1000
	p.Intensity = -50
	p.Density = 500
	p.Direction = "diagonal"
	clamped := Clamp("plasma", p)
	if clamped.Speed != 100 || clamped.Intensity != 0 || clamped.Density != 100 {
		t.Fatalf("clamped numerics = %+v", clamped)
	}
	if clamped.Direction != "none" {
		t.Fatalf("clamped direction = %q, want the plasma default", clamped.Direction)
	}
	if got := Clamp("unknown", p); got != p {
		t.Fatalf("Clamp of an unknown effect changed params: %+v", got)
	}
	if got := WithDefaults("scan", Params{Direction: ""}); got.Direction != DirectionLeft {
		t.Fatalf("WithDefaults direction = %q, want left", got.Direction)
	}
}

func TestRendererBoundsSafety(t *testing.T) {
	sizes := [][2]int{{1, 1}, {1, 5}, {7, 3}, {64, 32}, {128, 64}, {256, 128}, {3, 64}}
	for _, name := range nativeEffectNames {
		r, ok := NewRendererFor(name)
		if !ok {
			t.Fatalf("no renderer for %q", name)
		}
		for _, size := range sizes {
			w, h := size[0], size[1]
			img := r.Frame(w, h, testParams(), 5*time.Second)
			if img == nil {
				t.Fatalf("%s: nil frame at %dx%d", name, w, h)
			}
			if img.Rect.Dx() != w || img.Rect.Dy() != h {
				t.Fatalf("%s: frame %v at %dx%d", name, img.Rect, w, h)
			}
			for i := 3; i < len(img.Pix); i += 4 {
				if img.Pix[i] != 255 {
					t.Fatalf("%s: uncovered pixel at %dx%d offset %d", name, w, h, i)
				}
			}
		}
	}
	// Degenerate sizes are clamped up to one pixel instead of panicking.
	img := NewRenderer(mustEffect(t, "plasma")).Frame(0, -4, testParams(), 0)
	if img.Rect.Dx() != 1 || img.Rect.Dy() != 1 {
		t.Fatalf("degenerate size frame = %v", img.Rect)
	}
}

func mustEffect(t *testing.T, name string) Effect {
	t.Helper()
	_, eff, ok := Lookup(name)
	if !ok {
		t.Fatalf("effect %q is not registered", name)
	}
	return eff
}

func TestRendererReusesScratchBuffer(t *testing.T) {
	r := NewRenderer(mustEffect(t, "noise"))
	first := r.Frame(64, 32, testParams(), 0)
	second := r.Frame(64, 32, testParams(), time.Second)
	if first != second {
		t.Fatal("renderer reallocated its scratch buffer for an unchanged size")
	}
	third := r.Frame(32, 32, testParams(), time.Second)
	if third == first {
		t.Fatal("renderer did not resize its scratch buffer for a new size")
	}
	if r.Scratch() != third {
		t.Fatal("Scratch() does not report the current buffer")
	}
}

func TestRendererIsDeterministic(t *testing.T) {
	for _, name := range nativeEffectNames {
		p := testParams()
		first := renderBytes(name, 64, 32, p, 3*time.Second)
		again := renderBytes(name, 64, 32, p, 3*time.Second)
		if !bytes.Equal(first, again) {
			t.Fatalf("%s: repeated renders differ", name)
		}
		// Time below one quantum quantizes to the same tick.
		sub := renderBytes(name, 64, 32, p, 3*time.Second+TickInterval/2)
		if !bytes.Equal(first, sub) {
			t.Fatalf("%s: sub-tick elapsed changed the frame", name)
		}
		later := renderBytes(name, 64, 32, p, 3*time.Second+10*TickInterval)
		if bytes.Equal(first, later) {
			t.Fatalf("%s: frame did not change on a later tick", name)
		}
		other := p
		other.Seed = 1234
		if bytes.Equal(first, renderBytes(name, 64, 32, other, 3*time.Second)) {
			t.Fatalf("%s: frame did not change with a different seed", name)
		}
	}
}

func TestEffectColorsFollowPalette(t *testing.T) {
	p := testParams()
	p.Palette = DefaultPalette()
	alt := p
	alt.Palette = altPalette()
	for _, name := range nativeEffectNames {
		// sparkle is sparse at a single tick, so compare over several ticks.
		differs := false
		for _, elapsed := range []time.Duration{0, 4 * time.Second, 12 * time.Second} {
			if !bytes.Equal(renderBytes(name, 64, 32, p, elapsed), renderBytes(name, 64, 32, alt, elapsed)) {
				differs = true
				break
			}
		}
		if !differs {
			t.Fatalf("%s: frame ignores the palette", name)
		}
	}
}

// goldenEffectHashes pins one hash per (effect, tick, seed) so an accidental
// change to the deterministic render contract fails loudly.
var goldenEffectHashes = map[string]string{
	"fire/0/7":      "9235c80c6273e358",
	"fire/1/7":      "a598b7c17171489b",
	"fire/0/42":     "1f30ac1aff0ef206",
	"fire/1/42":     "6410c0f8aa6d89a0",
	"gradient/0/7":  "6631a24a03d6d570",
	"gradient/1/7":  "10d374915a48fae2",
	"gradient/0/42": "780431d79b1cdfc9",
	"gradient/1/42": "97368dfd29fbfffc",
	"noise/0/7":     "5c4f56565396df76",
	"noise/1/7":     "46d59dd1531e23ee",
	"noise/0/42":    "0551a9f829895493",
	"noise/1/42":    "1d03b04174531cd3",
	"plasma/0/7":    "12b60ec9091b2b42",
	"plasma/1/7":    "2be39e09e144fc8c",
	"plasma/0/42":   "aea329029741e987",
	"plasma/1/42":   "bd3184baff10b786",
	"scan/0/7":      "0ec4368e980db8e8",
	"scan/1/7":      "4cc6fa821c9265cc",
	"scan/0/42":     "77bfde0da86b8247",
	"scan/1/42":     "d165192c990d98d7",
	"sparkle/0/7":   "021ba11d570c2296",
	"sparkle/1/7":   "99105d0b9e2f8144",
	"sparkle/0/42":  "5a0395c3ff040fa7",
	"sparkle/1/42":  "e51ed5b349aeb82e",
}

func TestGoldenEffectHashes(t *testing.T) {
	seeds := []int64{7, 42}
	ticks := []int64{0, 1}
	for _, name := range nativeEffectNames {
		seen := map[string]bool{}
		for _, seed := range seeds {
			for _, tick := range ticks {
				p := testParams()
				p.Seed = seed
				r, _ := NewRendererFor(name)
				img := r.Frame(64, 32, p, time.Duration(tick)*TickInterval)
				got := frameHash(img)
				seen[got] = true
				key := name + "/" + itoa(tick) + "/" + itoa(seed)
				want, ok := goldenEffectHashes[key]
				if !ok {
					t.Fatalf("missing golden entry %q (got %q)", key, got)
				}
				if want == "" {
					t.Logf("GOLDEN %q: %q", key, got)
					continue
				}
				if got != want {
					t.Fatalf("%s: hash = %q, want %q", key, got, want)
				}
			}
		}
		if len(seen) < 2 {
			t.Fatalf("%s: ticks/seeds produced a single hash", name)
		}
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestSteadyStateRenderingAllocatesNothing(t *testing.T) {
	p := testParams()
	for _, name := range nativeEffectNames {
		r, _ := NewRendererFor(name)
		r.Frame(64, 32, p, 0) // warm the scratch buffer
		allocs := testing.AllocsPerRun(50, func() {
			r.Frame(64, 32, p, time.Second)
		})
		if allocs != 0 {
			t.Fatalf("%s: steady-state rendering allocated %.2f objects per frame", name, allocs)
		}
	}
}

// Documented budgets: the draw pass must stay under 0.5 ms at 64x32, 2 ms at
// 128x64 and 6 ms at 256x128 on the reference host. The guard fails when a
// built-in exceeds its budget by more than the documented 2x CI margin.
var effectBudgets = []struct {
	width, height int
	budget        time.Duration
}{
	{64, 32, 500 * time.Microsecond},
	{128, 64, 2 * time.Millisecond},
	{256, 128, 6 * time.Millisecond},
}

const budgetCIMargin = 2.0

func TestEffectPerformanceBudgets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing guard in short mode")
	}
	p := testParams()
	const rounds = 20
	for _, name := range nativeEffectNames {
		for _, b := range effectBudgets {
			r, _ := NewRendererFor(name)
			r.Frame(b.width, b.height, p, 0)
			start := time.Now()
			for i := 0; i < rounds; i++ {
				r.Frame(b.width, b.height, p, time.Duration(i)*TickInterval)
			}
			perFrame := time.Since(start) / rounds
			limit := time.Duration(float64(b.budget) * budgetCIMargin)
			if perFrame > limit {
				t.Errorf("%s at %dx%d took %v per frame, budget %v (limit with margin %v)", name, b.width, b.height, perFrame, b.budget, limit)
			}
		}
	}
}

func BenchmarkEffectFrame(b *testing.B) {
	p := testParams()
	for _, name := range nativeEffectNames {
		for _, size := range effectBudgets {
			b.Run(name+"/"+itoa(int64(size.width))+"x"+itoa(int64(size.height)), func(b *testing.B) {
				r, _ := NewRendererFor(name)
				r.Frame(size.width, size.height, p, 0)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					r.Frame(size.width, size.height, p, time.Duration(i)*TickInterval)
				}
			})
		}
	}
}

func TestConcurrentRenderersAreIndependent(t *testing.T) {
	p := testParams()
	want := map[string][]byte{}
	for _, name := range nativeEffectNames {
		want[name] = renderBytes(name, 64, 32, p, 9*time.Second)
	}
	var wg sync.WaitGroup
	errs := make(chan string, len(nativeEffectNames)*4)
	for i := 0; i < 4; i++ {
		for _, name := range nativeEffectNames {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				r, _ := NewRendererFor(name)
				for j := 0; j < 5; j++ {
					img := r.Frame(64, 32, p, 9*time.Second)
					if !bytes.Equal(img.Pix, want[name]) {
						errs <- name + ": concurrent frame differs"
						return
					}
				}
			}(name)
		}
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Error(msg)
	}
}

func TestMixPaletteEndpoints(t *testing.T) {
	pal := DefaultPalette()
	if got := mixPalette(pal, 0); got != pal.Background {
		t.Fatalf("mixPalette(0) = %v, want background", got)
	}
	if got := mixPalette(pal, 0.5); got != pal.Accent {
		t.Fatalf("mixPalette(0.5) = %v, want accent", got)
	}
	if got := mixPalette(pal, 1); got != pal.Text {
		t.Fatalf("mixPalette(1) = %v, want text", got)
	}
	if got := mixPalette(pal, 2); got != pal.Text {
		t.Fatalf("mixPalette above 1 = %v, want text", got)
	}
	want := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	if got := lerpRGBA(color.RGBA{}, want, 1); got != want {
		t.Fatalf("lerpRGBA = %v, want %v", got, want)
	}
}
