package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestValidatePixelArtPayloadValid(t *testing.T) {
	// build 8x8 valid payload
	row := make([]int, 8)
	frames := make([][][]int, 1)
	frames[0] = make([][]int, 8)
	for i := 0; i < 8; i++ {
		frames[0][i] = append([]int(nil), row...)
	}
	payload := map[string]interface{}{"palette": []string{"#000000", "#FF0000"}, "frames": frames, "duration_ms": 500}
	b, _ := json.Marshal(payload)
	_, err := ValidatePixelArtPayload(b, 8, 8, 1)
	if err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
}

func TestValidateDimensionMismatch(t *testing.T) {
	raw := `{"palette":["#000000","#FF0000"],"frames":[[[0,0],[0,1]]],"duration_ms":500}`
	_, err := ValidatePixelArtPayload([]byte(raw), 32, 32, 1)
	if err == nil || !strings.Contains(err.Error(), "rows") {
		t.Fatalf("expected dimension mismatch, got %v", err)
	}
}

func TestValidatePaletteIndexOutOfRange(t *testing.T) {
	row0 := make([]int, 8)
	row1 := make([]int, 8)
	row1[1] = 9
	frames := [][][]int{{row0, row0, row0, row0, row0, row0, row0, row1}}
	// actually need 8 rows
	frames2 := [][][]int{make([][]int, 8)}
	for i := 0; i < 8; i++ {
		frames2[0][i] = make([]int, 8)
	}
	frames2[0][1][1] = 9
	b, _ := json.Marshal(map[string]interface{}{"palette": []string{"#000000", "#FF0000"}, "frames": frames2[0], "duration_ms": 500})
	// use correct nesting
	payload := map[string]interface{}{"palette": []string{"#000000", "#FF0000"}, "frames": []interface{}{frames2[0]}, "duration_ms": 500}
	b, _ = json.Marshal(payload)
	_ = frames
	_, err := ValidatePixelArtPayload(b, 8, 8, 1)
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected out of range, got %v", err)
	}
}

func TestValidateInvalidColor(t *testing.T) {
	raw := `{"palette":["#GGGGGG","#FF0000"],"frames":[[[0,0],[0,1]]],"duration_ms":500}`
	_, err := ValidatePixelArtPayload([]byte(raw), 2, 2, 1)
	if err == nil || !strings.Contains(err.Error(), "not a valid") {
		t.Fatalf("expected invalid color, got %v", err)
	}
}

func TestValidateTooManyFrames(t *testing.T) {
	frames := make([][][]int, 10)
	for i := range frames {
		frames[i] = [][]int{{0, 0}, {0, 0}}
	}
	payload := map[string]interface{}{"palette": []string{"#000000", "#FFFFFF"}, "frames": frames, "duration_ms": 500}
	b, _ := json.Marshal(payload)
	_, err := ValidatePixelArtPayload(b, 2, 2, 10)
	if err == nil {
		t.Fatalf("expected too many frames rejected")
	}
}

func TestValidateMarkdownWrapped(t *testing.T) {
	row := make([]int, 8)
	f := make([][]int, 8)
	for i := range f {
		f[i] = append([]int(nil), row...)
	}
	inner, _ := json.Marshal(map[string]interface{}{"palette": []string{"#000000", "#FF0000"}, "frames": []interface{}{f}, "duration_ms": 500})
	raw := "```json\n" + string(inner) + "\n```"
	p, err := ValidatePixelArtPayload([]byte(raw), 8, 8, 1)
	if err != nil {
		t.Fatalf("markdown wrapped failed: %v", err)
	}
	if p.Palette[1] != "#FF0000" {
		t.Fatalf("palette not parsed")
	}
}

func TestValidateOversized(t *testing.T) {
	big := make([]byte, 65*1024)
	for i := range big {
		big[i] = 'a'
	}
	_, err := ValidatePixelArtPayload(big, 2, 2, 1)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected oversized rejected, got %v", err)
	}
}

func TestBuildPromptContainsFewShot(t *testing.T) {
	p := BuildPixelArtPrompt("hello", 32, 32, "", 1, "")
	if !strings.Contains(p, "Example (2x2") || !strings.Contains(p, "red dot") {
		t.Fatalf("prompt missing few-shot example")
	}
	_ = fmt.Sprintf("%v", p)
}

func TestBuildPromptRefine(t *testing.T) {
	p := BuildPixelArtPrompt("more rain", 32, 32, "", 1, `{"palette":["#000000"],"frames":[[[0]]]}`)
	if !strings.Contains(p, "Current draft") {
		t.Fatalf("refine prompt missing current draft")
	}
}
