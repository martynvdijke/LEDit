package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/composition"
	"ledit/ent/generalsettings"
	"ledit/ent/pixelart"
	"ledit/ent/playlist"
	"ledit/ent/scene"
	"ledit/ent/theme"
	"ledit/render"
)

// ---------------------------------------------------------------------------
// Bundle format constants
// ---------------------------------------------------------------------------

const (
	artifactSchemaVersion = 1

	artifactKindTheme        = "theme"
	artifactKindLayout       = "layout"
	artifactKindPlaylist     = "playlist"
	artifactKindScene        = "scene"
	artifactKindTextSlide    = "text-slide"
	artifactKindPixelArt     = "pixel-art"
	artifactKindEffectPreset = "effect-preset"

	artifactEntryManifest = "manifest.json"
	artifactEntryPayload  = "payload.json"
	artifactEntryPreview  = "preview.png"

	artifactMaxBodyBytes     = 8 << 20
	artifactMaxUncompressed  = 16 << 20
	artifactMaxEntries       = 32
	artifactMaxPreviewBytes  = 1 << 20
	artifactMaxPreviewDim    = 1024
	artifactCatalogMaxBytes  = 256 << 10
	artifactMaxNameLen       = 128
	artifactMaxMetaLen       = 64
	artifactNameConflictCode = "name_conflict"
)

// artReMatch reports whether raw matches the given pattern (nil pattern = ok).
var artifactHexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func artifactHexOK(raw string) bool { return artifactHexColor.MatchString(raw) }

// Stable error codes from design D10.
const (
	errCodeUnauthorized      = "unauthorized"
	errCodeInvalidBundle     = "invalid_bundle"
	errCodeInvalidZip        = "invalid_zip"
	errCodeInvalidJSON       = "invalid_json"
	errCodeMissingManifest   = "missing_manifest"
	errCodeMissingPayload    = "missing_payload"
	errCodeUnsupportedVer    = "unsupported_version"
	errCodeUnsupportedKind   = "unsupported_kind"
	errCodeBundleTooLarge    = "bundle_too_large"
	errCodeInvalidPath       = "invalid_path"
	errCodeChecksumMismatch  = "checksum_mismatch"
	errCodeNameConflict      = "name_conflict"
	errCodeValidationFailed  = "validation_failed"
	errCodeSecretNotAllowed  = "secret_not_allowed"
	errCodeFetchFailed       = "fetch_failed"
	errCodeHostNotAllowed    = "host_not_allowed"
	errCodeImportFailed      = "import_failed"
	errCodeInvalidURL        = "invalid_url"
	errCodePreviewNotAllowed = "preview_not_allowed"
)

// artifactError is the stable machine-readable error body: it never carries a
// secret or a filesystem path in its message.
type artifactError struct {
	Code    string
	Message string
	Status  int
	Details map[string]any
}

func (e *artifactError) Error() string { return e.Message }

func artifactErr(code, message string) *artifactError {
	return &artifactError{Code: code, Message: message, Status: artifactErrorStatus(code)}
}

func artifactErrorStatus(code string) int {
	switch code {
	case errCodeUnauthorized:
		return 401
	case errCodeNameConflict:
		return 409
	case errCodeBundleTooLarge:
		return 413
	case errCodeImportFailed:
		return 500
	case errCodeFetchFailed, errCodeHostNotAllowed:
		return 502
	default:
		return 400
	}
}

func (e *artifactError) withDetails(d map[string]any) *artifactError {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	for k, v := range d {
		e.Details[k] = v
	}
	return e
}

func writeArtifactError(c *gin.Context, e *artifactError) {
	if e == nil {
		e = artifactErr(errCodeImportFailed, "import failed")
	}
	buf := bytes.NewBufferString(`{"error":`)
	buf.WriteString(strconv.Quote(e.Code))
	buf.WriteString(`,"message":`)
	buf.WriteString(strconv.Quote(e.Message))
	if len(e.Details) > 0 {
		if b, err := json.Marshal(e.Details); err == nil {
			buf.WriteString(`,"details":`)
			buf.Write(b)
		}
	}
	buf.WriteString("}")
	c.Header("Cache-Control", "no-store")
	c.Data(e.Status, "application/json; charset=utf-8", buf.Bytes())
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ArtifactRequirement is one typed reference the payload expects on the target.
type ArtifactRequirement struct {
	Kind string `json:"kind"`
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
}

// ArtifactManifest is manifest.json.
type ArtifactManifest struct {
	Schema        int                   `json:"schema"`
	Kind          string                `json:"kind"`
	Name          string                `json:"name"`
	Version       string                `json:"version"`
	Author        string                `json:"author"`
	Description   string                `json:"description"`
	CreatedWith   string                `json:"created_with"`
	Requires      []ArtifactRequirement `json:"requires"`
	PayloadSHA256 string                `json:"payload_sha256"`
}

// ArtifactPayload is payload.json.
type ArtifactPayload struct {
	Kind   string         `json:"kind"`
	Entity map[string]any `json:"entity"`
}

// ArtifactWarning is a non-fatal import note.
type ArtifactWarning struct {
	Code    string `json:"code"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// ArtifactConflict identifies the row an import collides with.
type ArtifactConflict struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	ExistingID int    `json:"existing_id"`
}

// ArtifactImportResult is the stable success/dry-run contract.
type ArtifactImportResult struct {
	Imported bool              `json:"imported"`
	Kind     string            `json:"kind"`
	Name     string            `json:"name"`
	ID       int               `json:"id"`
	Replaced bool              `json:"replaced"`
	Skipped  bool              `json:"skipped"`
	Warnings []ArtifactWarning `json:"warnings"`
	Action   string            `json:"action,omitempty"`
	Conflict *ArtifactConflict `json:"conflict,omitempty"`
}

type artifactImportOptions struct {
	Name       string
	OnConflict string
	SHA256     string
	DryRun     bool
	Source     string
}

// artifactKindSpec is one entry of the closed kind registry. Kind selects code,
// never a path, URL, or interpreter.
type artifactKindSpec struct {
	Kind      string
	NameField string
	Unique    bool
	Fields    map[string]bool
	Validate  func(entity map[string]any) error
	Requires  func(entity map[string]any) []ArtifactRequirement
	Preview   func(entity map[string]any) ([]byte, error)
	// Load reads an existing row and marshals it to an entity map (export).
	Load func(ctx context.Context, db *ent.Client, id int) (map[string]any, error)
	// Existing returns the id of a same-named row, or 0 when none exists.
	Existing func(ctx context.Context, db *ent.Client, name string) (int, error)
	// Create inserts a new row inside the import transaction.
	Create func(ctx context.Context, tx *ent.Tx, entity map[string]any, name string) (int, error)
	// Update rewrites an existing row in place, preserving its id.
	Update func(ctx context.Context, tx *ent.Tx, id int, entity map[string]any, name string) error
}

var executableFields = map[string]bool{
	"exec": true, "script": true, "command": true, "entrypoint": true, "hook": true,
}

// ---------------------------------------------------------------------------
// Deterministic payload serialization
// ---------------------------------------------------------------------------

// canonicalEntity mirrors ExportBundle's row-to-map marshaling, strips secrets,
// and removes instance-local fields so the same artifact serializes identically
// on every instance.
func canonicalEntity(raw map[string]any) map[string]any {
	out := stripSecrets(raw, false)
	delete(out, "id")
	delete(out, "created_at")
	delete(out, "updated_at")
	return out
}

// canonicalPayloadBytes serializes {"kind":..., "entity":{...}}. Because
// encoding/json sorts map keys, the bytes are deterministic.
func canonicalPayloadBytes(kind string, entity map[string]any) ([]byte, error) {
	return json.Marshal(ArtifactPayload{Kind: kind, Entity: entity})
}

func payloadSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func artifactRowMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// entity accessors tolerate the float64 numbers produced by JSON decoding.
func entityString(e map[string]any, key string) string {
	switch v := e[key].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func entityInt(e map[string]any, key string) int {
	switch v := e[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		n, err := v.Int64()
		if err == nil {
			return int(n)
		}
	}
	return 0
}

func entityFloat(e map[string]any, key string) float64 {
	switch v := e[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		f, err := v.Float64()
		if err == nil {
			return f
		}
	}
	return 0
}

func entityBool(e map[string]any, key string) bool {
	switch v := e[key].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "on"
	}
	return false
}

// hasEntityKey reports whether the key is present (even with a zero value).
func hasEntityKey(e map[string]any, key string) bool {
	_, ok := e[key]
	return ok
}

func entityIntPtr(e map[string]any, key string) *int {
	if !hasEntityKey(e, key) || e[key] == nil {
		return nil
	}
	v := entityInt(e, key)
	return &v
}

// findExecutableField scans a payload for any field naming executable behavior.
func findExecutableField(v any) string {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if executableFields[strings.ToLower(k)] {
				return k
			}
			if f := findExecutableField(val); f != "" {
				return f
			}
		}
	case []any:
		for _, item := range t {
			if f := findExecutableField(item); f != "" {
				return f
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Zip container
// ---------------------------------------------------------------------------

type artifactBundle struct {
	Manifest  *ArtifactManifest
	Entity    map[string]any
	Payload   []byte
	Preview   []byte
	EntryPath []string
}

// writeArtifactBundle builds the zip container with a fixed entry list.
func writeArtifactBundle(manifest *ArtifactManifest, entity map[string]any, preview []byte) ([]byte, error) {
	payloadBytes, err := canonicalPayloadBytes(manifest.Kind, entity)
	if err != nil {
		return nil, err
	}
	manifest.PayloadSHA256 = payloadSHA256(payloadBytes)
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	mf, err := zw.Create(artifactEntryManifest)
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(mf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(manifest); err != nil {
		return nil, err
	}
	pf, err := zw.Create(artifactEntryPayload)
	if err != nil {
		return nil, err
	}
	if _, err := pf.Write(payloadBytes); err != nil {
		return nil, err
	}
	if len(preview) > 0 {
		xf, err := zw.Create(artifactEntryPreview)
		if err != nil {
			return nil, err
		}
		if _, err := xf.Write(preview); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// readArtifactBundle parses a bundle, enforcing every container cap before any
// database work happens. expectedSHA, when set, must match the whole file.
func readArtifactBundle(data []byte, expectedSHA string) (*artifactBundle, *artifactError) {
	if len(data) > artifactMaxBodyBytes {
		return nil, artifactErr(errCodeBundleTooLarge, "bundle exceeds the 8 MiB body cap")
	}
	if expectedSHA != "" && !strings.EqualFold(strings.TrimSpace(expectedSHA), sha256Hex(data)) {
		return nil, artifactErr(errCodeChecksumMismatch, "bundle does not match the expected sha256")
	}
	if len(data) < 2 || data[0] != 0x50 || data[1] != 0x4B {
		return nil, artifactErr(errCodeInvalidZip, "bundle is not a zip archive")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, artifactErr(errCodeInvalidZip, "bundle zip could not be read")
	}
	if len(zr.File) > artifactMaxEntries {
		return nil, artifactErr(errCodeBundleTooLarge, fmt.Sprintf("bundle has more than %d entries", artifactMaxEntries))
	}
	var total uint64
	for _, f := range zr.File {
		clean := filepath.Clean(f.Name)
		if clean != f.Name || strings.Contains(clean, "..") || strings.HasPrefix(clean, "/") {
			return nil, artifactErr(errCodeInvalidPath, "bundle contains an unsafe entry path")
		}
		if clean != artifactEntryManifest && clean != artifactEntryPayload && clean != artifactEntryPreview {
			return nil, artifactErr(errCodeInvalidPath, "bundle contains an entry outside the allowed set")
		}
		total += f.UncompressedSize64
		if total > artifactMaxUncompressed {
			return nil, artifactErr(errCodeBundleTooLarge, "bundle uncompressed size exceeds the 16 MiB cap")
		}
	}

	b := &artifactBundle{}
	var manifestRaw, payloadRaw []byte
	for _, f := range zr.File {
		switch f.Name {
		case artifactEntryManifest:
			rc, err := f.Open()
			if err != nil {
				return nil, artifactErr(errCodeInvalidZip, "manifest entry could not be opened")
			}
			manifestRaw, err = io.ReadAll(io.LimitReader(rc, artifactMaxUncompressed))
			rc.Close()
			if err != nil {
				return nil, artifactErr(errCodeInvalidZip, "manifest entry could not be read")
			}
		case artifactEntryPayload:
			rc, err := f.Open()
			if err != nil {
				return nil, artifactErr(errCodeInvalidZip, "payload entry could not be opened")
			}
			payloadRaw, err = io.ReadAll(io.LimitReader(rc, artifactMaxUncompressed))
			rc.Close()
			if err != nil {
				return nil, artifactErr(errCodeInvalidZip, "payload entry could not be read")
			}
		case artifactEntryPreview:
			rc, err := f.Open()
			if err != nil {
				return nil, artifactErr(errCodeInvalidZip, "preview entry could not be opened")
			}
			b.Preview, err = io.ReadAll(io.LimitReader(rc, artifactMaxPreviewBytes+1))
			rc.Close()
			if err != nil {
				return nil, artifactErr(errCodeInvalidZip, "preview entry could not be read")
			}
		}
	}
	if manifestRaw == nil {
		return nil, artifactErr(errCodeMissingManifest, "bundle is missing manifest.json")
	}
	if payloadRaw == nil {
		return nil, artifactErr(errCodeMissingPayload, "bundle is missing payload.json")
	}
	if len(b.Preview) > artifactMaxPreviewBytes {
		return nil, artifactErr(errCodePreviewNotAllowed, "preview.png exceeds the 1 MiB cap")
	}
	if len(b.Preview) > 0 {
		cfg, format, err := image.DecodeConfig(bytes.NewReader(b.Preview))
		if err != nil || format != "png" {
			return nil, artifactErr(errCodePreviewNotAllowed, "preview.png is not a valid PNG")
		}
		if cfg.Width > artifactMaxPreviewDim || cfg.Height > artifactMaxPreviewDim {
			return nil, artifactErr(errCodePreviewNotAllowed, "preview.png exceeds 1024x1024")
		}
	}

	var manifest ArtifactManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return nil, artifactErr(errCodeInvalidJSON, "manifest.json is not valid JSON")
	}
	if manifest.PayloadSHA256 == "" || !strings.EqualFold(manifest.PayloadSHA256, payloadSHA256(payloadRaw)) {
		return nil, artifactErr(errCodeChecksumMismatch, "payload.json does not match the manifest checksum")
	}
	if manifest.Schema != artifactSchemaVersion {
		if manifest.Schema > artifactSchemaVersion {
			return nil, artifactErr(errCodeUnsupportedVer, fmt.Sprintf("bundle schema %d is newer than this instance supports", manifest.Schema))
		}
		return nil, artifactErr(errCodeUnsupportedVer, fmt.Sprintf("bundle schema %d is not supported", manifest.Schema))
	}
	if manifest.Kind == artifactKindEffectPreset {
		return nil, artifactErr(errCodeUnsupportedKind, "effect-preset bundles are not supported by this version of LEDit")
	}
	if _, ok := artifactKinds[manifest.Kind]; !ok {
		return nil, artifactErr(errCodeUnsupportedKind, fmt.Sprintf("kind %q is not supported", manifest.Kind))
	}
	name := strings.TrimSpace(manifest.Name)
	if name == "" {
		return nil, artifactErr(errCodeValidationFailed, "manifest name is required")
	}
	if len(name) > artifactMaxNameLen {
		return nil, artifactErr(errCodeValidationFailed, fmt.Sprintf("manifest name must be at most %d characters", artifactMaxNameLen))
	}
	if len(manifest.Version) > artifactMaxMetaLen || len(manifest.CreatedWith) > artifactMaxMetaLen {
		return nil, artifactErr(errCodeValidationFailed, "manifest version and created_with must be short strings")
	}

	var payload ArtifactPayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return nil, artifactErr(errCodeInvalidJSON, "payload.json is not valid JSON")
	}
	if payload.Entity == nil {
		return nil, artifactErr(errCodeInvalidJSON, "payload.json is missing its entity object")
	}
	if payload.Kind != "" && payload.Kind != manifest.Kind {
		return nil, artifactErr(errCodeValidationFailed, "payload kind does not match the manifest kind")
	}
	manifest.Name = name
	b.Manifest = &manifest
	b.Entity = payload.Entity
	b.Payload = payloadRaw
	return b, nil
}

// ---------------------------------------------------------------------------
// Per-kind preparation, validation and reference scan
// ---------------------------------------------------------------------------

// prepareArtifactEntity rejects executable fields, strips secrets, drops
// unknown fields with warnings, and validates the remaining entity.
func prepareArtifactEntity(spec *artifactKindSpec, raw map[string]any) (map[string]any, []ArtifactWarning, *artifactError) {
	var warnings []ArtifactWarning
	if f := findExecutableField(raw); f != "" {
		return nil, nil, artifactErr(errCodeValidationFailed, fmt.Sprintf("payload contains an executable field %q; artifacts are declarative only", f))
	}
	stripped := stripSecrets(raw, false)
	if len(stripped) != len(raw) {
		warnings = append(warnings, ArtifactWarning{Code: "secret_stripped", Path: "payload.entity", Message: "secret fields were removed from the bundle"})
	}
	out := make(map[string]any, len(stripped))
	for k, v := range stripped {
		if !spec.Fields[k] {
			warnings = append(warnings, ArtifactWarning{Code: "unknown_field", Path: "payload.entity." + k, Message: fmt.Sprintf("field %q is not recognized by this kind schema and was dropped", k)})
			continue
		}
		out[k] = v
	}
	if err := spec.Validate(out); err != nil {
		return nil, nil, artifactErr(errCodeValidationFailed, err.Error())
	}
	return out, warnings, nil
}

// entityVariableName renders the display name of an entity for manifest/export.
func artifactDisplayName(spec *artifactKindSpec, entity map[string]any) string {
	if spec.NameField != "" {
		if n := strings.TrimSpace(entityString(entity, spec.NameField)); n != "" {
			return n
		}
	}
	if spec.Kind == artifactKindTextSlide {
		return artifactTextSlideName(entityString(entity, "content"))
	}
	return strings.ToTitle(strings.ReplaceAll(spec.Kind, "-", " "))
}

func artifactTextSlideName(content string) string {
	c := strings.Join(strings.Fields(content), " ")
	if c == "" {
		return "Text slide"
	}
	if len(c) > 60 {
		c = strings.TrimSpace(c[:60]) + "…"
	}
	return c
}

// artifactRefWarnings resolves payload references against the live instance and
// reports any that do not resolve. Unresolved refs are never fatal.
func (s *Server) artifactRefWarnings(c *gin.Context, reqs []ArtifactRequirement) []ArtifactWarning {
	var warnings []ArtifactWarning
	opts := s.bindingOptions(c)
	playlists := map[int]bool{}
	if rows, err := s.DB.Playlist.Query().All(c.Request.Context()); err == nil {
		for _, p := range rows {
			playlists[p.ID] = true
		}
	}
	seen := map[string]bool{}
	for _, r := range reqs {
		if r.Kind != "datasource" && r.Kind != "playlist" {
			continue
		}
		key := r.Kind + ":" + r.Type + ":" + r.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		if r.Kind == "playlist" {
			id, _ := strconv.Atoi(r.Name)
			if id > 0 && !playlists[id] {
				warnings = append(warnings, ArtifactWarning{Code: "ref_missing", Path: "requires", Message: fmt.Sprintf("playlist %d is not present on this instance", id)})
			}
			continue
		}
		list, ok := opts[r.Type]
		if !ok {
			warnings = append(warnings, ArtifactWarning{Code: "ref_missing", Path: "requires", Message: fmt.Sprintf("no %s datasource is configured", r.Type)})
			continue
		}
		id, _ := strconv.Atoi(strings.TrimPrefix(r.Name, "#"))
		found := false
		for _, o := range list {
			if o.ID == id {
				found = true
				break
			}
		}
		if !found {
			warnings = append(warnings, ArtifactWarning{Code: "ref_missing", Path: "requires", Message: fmt.Sprintf("%s source #%d is not configured", r.Type, id)})
		}
	}
	return warnings
}

// collectDatasourceRefs builds the datasource requirements for a set of
// source_type/source_id pairs.
func collectDatasourceRefs(out *[]ArtifactRequirement, seen map[string]bool, sourceType string, sourceID int) {
	if sourceType == "" {
		return
	}
	key := sourceType + ":" + strconv.Itoa(sourceID)
	if seen[key] {
		return
	}
	seen[key] = true
	*out = append(*out, ArtifactRequirement{Kind: "datasource", Type: sourceType, Name: "#" + strconv.Itoa(sourceID)})
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

// ExportArtifact loads one artifact and returns its bundle bytes.
func (s *Server) ExportArtifact(kind string, id int) ([]byte, *ArtifactManifest, *artifactError) {
	spec, ok := artifactKinds[kind]
	if !ok {
		if kind == artifactKindEffectPreset {
			return nil, nil, artifactErr(errCodeUnsupportedKind, "effect-preset bundles are not supported by this version of LEDit")
		}
		return nil, nil, artifactErr(errCodeUnsupportedKind, fmt.Sprintf("kind %q is not supported", kind))
	}
	raw, err := spec.Load(s.Ctx, s.DB, id)
	if err != nil {
		return nil, nil, artifactErr(errCodeValidationFailed, fmt.Sprintf("%s #%d was not found", kind, id))
	}
	if kind == artifactKindTheme && entityBool(raw, "built_in") {
		return nil, nil, artifactErr(errCodeValidationFailed, "built-in themes cannot be exported")
	}
	entity := canonicalEntity(raw)
	var preview []byte
	if spec.Preview != nil {
		preview, _ = spec.Preview(entity)
	}
	var requires []ArtifactRequirement
	if spec.Requires != nil {
		requires = spec.Requires(entity)
	}
	manifest := &ArtifactManifest{
		Schema:      artifactSchemaVersion,
		Kind:        kind,
		Name:        artifactDisplayName(spec, entity),
		Version:     "1.0.0",
		CreatedWith: BundleVersion,
		Requires:    requires,
	}
	data, err := writeArtifactBundle(manifest, entity, preview)
	if err != nil {
		return nil, nil, artifactErr(errCodeImportFailed, "failed to build the bundle")
	}
	return data, manifest, nil
}

// ---------------------------------------------------------------------------
// Import
// ---------------------------------------------------------------------------

// importArtifact validates a bundle and, unless dry-run, writes it in one
// transaction.
func (s *Server) importArtifact(c *gin.Context, data []byte, opts artifactImportOptions) (*ArtifactImportResult, *artifactError) {
	ctx := c.Request.Context()
	b, aerr := readArtifactBundle(data, opts.SHA256)
	if aerr != nil {
		return nil, aerr
	}
	spec := artifactKinds[b.Manifest.Kind]
	entity, warnings, aerr := prepareArtifactEntity(spec, b.Entity)
	if aerr != nil {
		return nil, aerr
	}
	var reqs []ArtifactRequirement
	if spec.Requires != nil {
		reqs = spec.Requires(entity)
	}
	warnings = append(warnings, s.artifactRefWarnings(c, reqs)...)
	if warnings == nil {
		warnings = []ArtifactWarning{}
	}

	result := &ArtifactImportResult{
		Kind:     b.Manifest.Kind,
		Name:     b.Manifest.Name,
		Action:   "create",
		Warnings: warnings,
	}

	existingID := 0
	if spec.NameField != "" {
		if id, err := spec.Existing(ctx, s.DB, b.Manifest.Name); err == nil {
			existingID = id
		}
	}
	conflict := opts.OnConflict
	if existingID != 0 {
		switch conflict {
		case "":
			result.Action = "conflict"
			result.Conflict = &ArtifactConflict{Kind: b.Manifest.Kind, Name: b.Manifest.Name, ExistingID: existingID}
		case "skip":
			result.Action = "skip"
			result.Skipped = true
		case "replace":
			result.Action = "replace"
		case "rename":
			result.Action = "rename"
		default:
			return nil, artifactErr(errCodeValidationFailed, fmt.Sprintf("on_conflict %q must be one of rename, skip, replace", conflict))
		}
	} else if conflict != "" && conflict != "rename" && conflict != "skip" && conflict != "replace" {
		return nil, artifactErr(errCodeValidationFailed, fmt.Sprintf("on_conflict %q must be one of rename, skip, replace", conflict))
	}

	finalName := b.Manifest.Name
	if result.Action == "rename" {
		finalName = strings.TrimSpace(opts.Name)
		if finalName == "" {
			return nil, artifactErr(errCodeValidationFailed, "on_conflict=rename requires a name")
		}
		if len(finalName) > artifactMaxNameLen {
			return nil, artifactErr(errCodeValidationFailed, fmt.Sprintf("name must be at most %d characters", artifactMaxNameLen))
		}
		if spec.NameField != "" {
			if id, err := spec.Existing(ctx, s.DB, finalName); err == nil && id != 0 {
				return nil, artifactErr(errCodeNameConflict, "an artifact with the requested name already exists").withDetails(map[string]any{"kind": string(b.Manifest.Kind), "name": finalName, "existing_id": id})
			}
		}
		result.Action = "create"
	}
	if result.Action == "conflict" {
		return nil, artifactErr(errCodeNameConflict, "an artifact with this name already exists").withDetails(map[string]any{"kind": b.Manifest.Kind, "name": b.Manifest.Name, "existing_id": existingID})
	}
	result.Name = finalName

	if opts.DryRun {
		result.Conflict = nil
		return result, nil
	}
	if result.Action == "skip" {
		return result, nil
	}

	if result.Action == "replace" && b.Manifest.Kind == artifactKindTheme {
		if row, err := s.DB.Theme.Get(ctx, existingID); err == nil && row.BuiltIn {
			return nil, artifactErr(errCodeValidationFailed, "built-in themes cannot be replaced")
		}
	}

	tx, err := s.DB.Tx(ctx)
	if err != nil {
		return nil, artifactErr(errCodeImportFailed, "could not start the import transaction")
	}
	var newID int
	if result.Action == "replace" {
		if err := spec.Update(ctx, tx, existingID, entity, finalName); err != nil {
			_ = tx.Rollback()
			return nil, artifactErr(errCodeImportFailed, "failed to update the existing artifact")
		}
		newID = existingID
		result.Replaced = true
	} else {
		id, err := spec.Create(ctx, tx, entity, finalName)
		if err != nil {
			_ = tx.Rollback()
			return nil, artifactErr(errCodeImportFailed, "failed to create the artifact")
		}
		newID = id
	}
	if err := tx.Commit(); err != nil {
		return nil, artifactErr(errCodeImportFailed, "failed to commit the import")
	}
	result.Imported = true
	result.ID = newID

	if s.LogStore != nil {
		actor := "admin"
		if u, ok := s.CurrentUser(c); ok && u != nil && u.Username != "" {
			actor = u.Username
		}
		src := opts.Source
		if src == "" {
			src = "upload"
		}
		s.LogStore.Submit(time.Now(), "info", "artifacts",
			fmt.Sprintf("artifact import kind=%s name=%s actor=%s conflict=%s source=%s", b.Manifest.Kind, finalName, actor, conflictOrCreate(conflict), src), "")
	}
	switch b.Manifest.Kind {
	case artifactKindPlaylist, artifactKindScene, artifactKindPixelArt:
		republishHASelectsForAll(s)
	}
	return result, nil
}

func conflictOrCreate(s string) string {
	if s == "" {
		return "create"
	}
	return s
}

// ---------------------------------------------------------------------------
// Kind registry
// ---------------------------------------------------------------------------

var artifactKinds = buildArtifactKinds()

func fieldSet(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func buildArtifactKinds() map[string]*artifactKindSpec {
	kinds := map[string]*artifactKindSpec{}

	// ---- theme -----------------------------------------------------------
	kinds[artifactKindTheme] = &artifactKindSpec{
		Kind:      artifactKindTheme,
		NameField: "name",
		Unique:    true,
		Fields:    fieldSet("name", "bg_color", "accent_color", "text_color", "title", "font_size", "built_in", "is_default", "font_name"),
		Validate: func(e map[string]any) error {
			name := strings.TrimSpace(entityString(e, "name"))
			if name == "" {
				return errors.New("theme name is required")
			}
			if len(name) > 64 {
				return errors.New("theme name must be at most 64 characters")
			}
			for _, k := range []string{"bg_color", "accent_color", "text_color"} {
				v := entityString(e, k)
				if v != "" && !artifactHexOK(v) {
					return fmt.Errorf("%s must be a #rrggbb color", k)
				}
			}
			if title := entityString(e, "title"); len(title) > 64 {
				return errors.New("title must be at most 64 characters")
			}
			fs := entityFloat(e, "font_size")
			if fs != 0 && (fs < 8 || fs > 100) {
				return errors.New("font_size must be between 8 and 100")
			}
			return nil
		},
		Preview: themePreview,
		Load: func(ctx context.Context, db *ent.Client, id int) (map[string]any, error) {
			row, err := db.Theme.Get(ctx, id)
			if err != nil {
				return nil, err
			}
			return artifactRowMap(row)
		},
		Existing: func(ctx context.Context, db *ent.Client, name string) (int, error) {
			row, err := db.Theme.Query().Where(theme.NameEQ(name)).Only(ctx)
			if err != nil {
				return 0, nil
			}
			return row.ID, nil
		},
		Create: func(ctx context.Context, tx *ent.Tx, e map[string]any, name string) (int, error) {
			row, err := tx.Theme.Create().
				SetName(name).
				SetBgColor(entityStringOr(e, "bg_color", "#282a36")).
				SetAccentColor(entityStringOr(e, "accent_color", "#50fa7b")).
				SetTextColor(entityStringOr(e, "text_color", "#8be9fd")).
				SetTitle(entityStringOr(e, "title", "CUSTOM")).
				SetFontSize(entityFloatOr(e, "font_size", 24)).
				SetFontName(entityString(e, "font_name")).
				SetBuiltIn(false).
				SetIsDefault(false).
				Save(ctx)
			if err != nil {
				return 0, err
			}
			return row.ID, nil
		},
		Update: func(ctx context.Context, tx *ent.Tx, id int, e map[string]any, name string) error {
			_, err := tx.Theme.UpdateOneID(id).
				SetName(name).
				SetBgColor(entityStringOr(e, "bg_color", "#282a36")).
				SetAccentColor(entityStringOr(e, "accent_color", "#50fa7b")).
				SetTextColor(entityStringOr(e, "text_color", "#8be9fd")).
				SetTitle(entityStringOr(e, "title", "CUSTOM")).
				SetFontSize(entityFloatOr(e, "font_size", 24)).
				SetFontName(entityString(e, "font_name")).
				Save(ctx)
			return err
		},
	}

	// ---- layout (Composition) -------------------------------------------
	kinds[artifactKindLayout] = &artifactKindSpec{
		Kind:      artifactKindLayout,
		NameField: "name",
		Fields:    fieldSet("name", "enabled", "mode", "rows", "cols", "gap", "padding", "background", "regions", "ttl_seconds"),
		Validate: func(e map[string]any) error {
			if strings.TrimSpace(entityString(e, "name")) == "" {
				return errors.New("layout name is required")
			}
			if len(entityString(e, "name")) > 128 {
				return errors.New("layout name must be at most 128 characters")
			}
			mode := entityStringOr(e, "mode", "absolute")
			if mode != "grid" && mode != "absolute" {
				return errors.New("mode must be grid or absolute")
			}
			rows, cols := entityIntOr(e, "rows", 1), entityIntOr(e, "cols", 1)
			if rows < 1 || rows > 8 {
				return errors.New("rows must be between 1 and 8")
			}
			if cols < 1 || cols > 8 {
				return errors.New("cols must be between 1 and 8")
			}
			gap, padding := entityInt(e, "gap"), entityInt(e, "padding")
			if bg := entityString(e, "background"); bg != "" && !artifactHexOK(bg) {
				return errors.New("background must be a #rrggbb color")
			}
			if ttl := entityInt(e, "ttl_seconds"); ttl < 0 {
				return errors.New("ttl_seconds must not be negative")
			}
			regions := entityStringOr(e, "regions", "[]")
			if err := validateRegionConfig(regions, 0, 0, rows, cols, gap, padding); err != nil {
				return err
			}
			return nil
		},
		Requires: func(e map[string]any) []ArtifactRequirement {
			var out []ArtifactRequirement
			seen := map[string]bool{}
			for _, r := range datasource.ParseRegions(entityStringOr(e, "regions", "[]")) {
				collectDatasourceRefs(&out, seen, r.SourceType, r.SourceID)
			}
			return out
		},
		Load: func(ctx context.Context, db *ent.Client, id int) (map[string]any, error) {
			row, err := db.Composition.Get(ctx, id)
			if err != nil {
				return nil, err
			}
			return artifactRowMap(row)
		},
		Existing: func(ctx context.Context, db *ent.Client, name string) (int, error) {
			row, err := db.Composition.Query().Where(composition.NameEQ(name)).First(ctx)
			if err != nil {
				return 0, nil
			}
			return row.ID, nil
		},
		Create: func(ctx context.Context, tx *ent.Tx, e map[string]any, name string) (int, error) {
			row, err := tx.Composition.Create().
				SetName(name).
				SetEnabled(entityBoolOr(e, "enabled", true)).
				SetMode(entityStringOr(e, "mode", "absolute")).
				SetRows(entityIntOr(e, "rows", 1)).
				SetCols(entityIntOr(e, "cols", 1)).
				SetGap(entityInt(e, "gap")).
				SetPadding(entityInt(e, "padding")).
				SetBackground(entityStringOr(e, "background", "#282a36")).
				SetRegions(entityStringOr(e, "regions", "[]")).
				SetTTLSeconds(entityInt(e, "ttl_seconds")).
				Save(ctx)
			if err != nil {
				return 0, err
			}
			if gs, err := tx.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(ctx); err == nil && gs != nil {
				_ = tx.GeneralSettings.UpdateOne(gs).AddCompositions(row).Exec(ctx)
			}
			return row.ID, nil
		},
		Update: func(ctx context.Context, tx *ent.Tx, id int, e map[string]any, name string) error {
			return tx.Composition.UpdateOneID(id).
				SetName(name).
				SetEnabled(entityBoolOr(e, "enabled", true)).
				SetMode(entityStringOr(e, "mode", "absolute")).
				SetRows(entityIntOr(e, "rows", 1)).
				SetCols(entityIntOr(e, "cols", 1)).
				SetGap(entityInt(e, "gap")).
				SetPadding(entityInt(e, "padding")).
				SetBackground(entityStringOr(e, "background", "#282a36")).
				SetRegions(entityStringOr(e, "regions", "[]")).
				SetTTLSeconds(entityInt(e, "ttl_seconds")).
				Exec(ctx)
		},
	}

	// ---- playlist --------------------------------------------------------
	kinds[artifactKindPlaylist] = &artifactKindSpec{
		Kind:      artifactKindPlaylist,
		NameField: "name",
		Fields:    fieldSet("name", "enabled", "items", "schedule_windows"),
		Validate: func(e map[string]any) error {
			if _, err := datasource.ParsePlaylistItems(entityStringOr(e, "items", "[]")); err != nil {
				return err
			}
			windows, err := ParseScheduleWindows(entityStringOr(e, "schedule_windows", "[]"))
			if err != nil {
				return err
			}
			return ValidateWindows(windows)
		},
		Requires: func(e map[string]any) []ArtifactRequirement {
			var out []ArtifactRequirement
			seen := map[string]bool{}
			items, err := datasource.ParsePlaylistItems(entityStringOr(e, "items", "[]"))
			if err != nil {
				return nil
			}
			for _, it := range items {
				collectDatasourceRefs(&out, seen, it.SourceType, it.SourceID)
			}
			return out
		},
		Load: func(ctx context.Context, db *ent.Client, id int) (map[string]any, error) {
			row, err := db.Playlist.Get(ctx, id)
			if err != nil {
				return nil, err
			}
			return artifactRowMap(row)
		},
		Existing: func(ctx context.Context, db *ent.Client, name string) (int, error) {
			row, err := db.Playlist.Query().Where(playlist.NameEQ(name)).First(ctx)
			if err != nil {
				return 0, nil
			}
			return row.ID, nil
		},
		Create: func(ctx context.Context, tx *ent.Tx, e map[string]any, name string) (int, error) {
			row, err := tx.Playlist.Create().
				SetName(name).
				SetEnabled(entityBoolOr(e, "enabled", true)).
				SetItems(entityStringOr(e, "items", "[]")).
				SetScheduleWindows(entityStringOr(e, "schedule_windows", "[]")).
				Save(ctx)
			if err != nil {
				return 0, err
			}
			if gs, err := tx.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(ctx); err == nil && gs != nil {
				_ = tx.GeneralSettings.UpdateOne(gs).AddPlaylists(row).Exec(ctx)
			}
			return row.ID, nil
		},
		Update: func(ctx context.Context, tx *ent.Tx, id int, e map[string]any, name string) error {
			return tx.Playlist.UpdateOneID(id).
				SetName(name).
				SetEnabled(entityBoolOr(e, "enabled", true)).
				SetItems(entityStringOr(e, "items", "[]")).
				SetScheduleWindows(entityStringOr(e, "schedule_windows", "[]")).
				Exec(ctx)
		},
	}

	// ---- scene -----------------------------------------------------------
	kinds[artifactKindScene] = &artifactKindSpec{
		Kind:      artifactKindScene,
		NameField: "name",
		Unique:    true,
		Fields:    fieldSet("name", "enabled", "triggers", "actions", "priority", "ttl_seconds"),
		Validate: func(e map[string]any) error {
			if strings.TrimSpace(entityString(e, "name")) == "" {
				return errors.New("scene name is required")
			}
			if len(entityString(e, "name")) > 128 {
				return errors.New("scene name must be at most 128 characters")
			}
			if _, err := parseSceneTriggers(entityStringOr(e, "triggers", "[]")); err != nil {
				return err
			}
			if _, err := parseSceneActions(entityStringOr(e, "actions", "{}")); err != nil {
				return err
			}
			if ttl := entityIntPtr(e, "ttl_seconds"); ttl != nil && (*ttl < 0 || *ttl > 86400) {
				return errors.New("ttl_seconds must be between 0 and 86400")
			}
			return nil
		},
		Requires: func(e map[string]any) []ArtifactRequirement {
			var out []ArtifactRequirement
			seen := map[string]bool{}
			actions, err := parseSceneActions(entityStringOr(e, "actions", "{}"))
			if err == nil {
				collectDatasourceRefs(&out, seen, actions.SourceType, actions.SourceID)
				for _, ctrl := range actions.Controls {
					collectDatasourceRefs(&out, seen, ctrl.SourceType, ctrl.SourceID)
				}
				if actions.PlaylistID != nil {
					out = append(out, ArtifactRequirement{Kind: "playlist", Name: strconv.Itoa(*actions.PlaylistID)})
				}
			}
			return out
		},
		Load: func(ctx context.Context, db *ent.Client, id int) (map[string]any, error) {
			row, err := db.Scene.Get(ctx, id)
			if err != nil {
				return nil, err
			}
			return artifactRowMap(row)
		},
		Existing: func(ctx context.Context, db *ent.Client, name string) (int, error) {
			row, err := db.Scene.Query().Where(scene.NameEQ(name)).Only(ctx)
			if err != nil {
				return 0, nil
			}
			return row.ID, nil
		},
		Create: func(ctx context.Context, tx *ent.Tx, e map[string]any, name string) (int, error) {
			row, err := tx.Scene.Create().
				SetName(name).
				SetEnabled(entityBoolOr(e, "enabled", true)).
				SetPriority(entityInt(e, "priority")).
				SetTriggers(entityStringOr(e, "triggers", "[]")).
				SetActions(entityStringOr(e, "actions", "{}")).
				SetNillableTTLSeconds(entityIntPtr(e, "ttl_seconds")).
				Save(ctx)
			if err != nil {
				return 0, err
			}
			if gs, err := tx.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(ctx); err == nil && gs != nil {
				_ = tx.GeneralSettings.UpdateOne(gs).AddScenes(row).Exec(ctx)
			}
			return row.ID, nil
		},
		Update: func(ctx context.Context, tx *ent.Tx, id int, e map[string]any, name string) error {
			return tx.Scene.UpdateOneID(id).
				SetName(name).
				SetEnabled(entityBoolOr(e, "enabled", true)).
				SetPriority(entityInt(e, "priority")).
				SetTriggers(entityStringOr(e, "triggers", "[]")).
				SetActions(entityStringOr(e, "actions", "{}")).
				SetNillableTTLSeconds(entityIntPtr(e, "ttl_seconds")).
				Exec(ctx)
		},
	}

	// ---- text-slide ------------------------------------------------------
	kinds[artifactKindTextSlide] = &artifactKindSpec{
		Kind:   artifactKindTextSlide,
		Fields: fieldSet("content", "color", "bg_color", "font_size"),
		Validate: func(e map[string]any) error {
			if strings.TrimSpace(entityString(e, "content")) == "" {
				return errors.New("text slide content is required")
			}
			if c := entityString(e, "color"); c != "" && !artifactHexOK(c) {
				return errors.New("color must be a #rrggbb color")
			}
			if c := entityString(e, "bg_color"); c != "" && !artifactHexOK(c) {
				return errors.New("bg_color must be a #rrggbb color")
			}
			if fs := entityInt(e, "font_size"); fs != 0 && (fs < 1 || fs > 400) {
				return errors.New("font_size must be between 1 and 400")
			}
			return nil
		},
		Load: func(ctx context.Context, db *ent.Client, id int) (map[string]any, error) {
			row, err := db.TextSlide.Get(ctx, id)
			if err != nil {
				return nil, err
			}
			return artifactRowMap(row)
		},
		Create: func(ctx context.Context, tx *ent.Tx, e map[string]any, name string) (int, error) {
			row, err := tx.TextSlide.Create().
				SetContent(entityString(e, "content")).
				SetColor(entityStringOr(e, "color", "#FFFFFF")).
				SetBgColor(entityStringOr(e, "bg_color", "#000000")).
				SetFontSize(entityIntOr(e, "font_size", 32)).
				Save(ctx)
			if err != nil {
				return 0, err
			}
			if gs, err := tx.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(ctx); err == nil && gs != nil {
				_ = tx.GeneralSettings.UpdateOne(gs).AddTextSlides(row).Exec(ctx)
			}
			return row.ID, nil
		},
		Update: func(ctx context.Context, tx *ent.Tx, id int, e map[string]any, name string) error {
			return tx.TextSlide.UpdateOneID(id).
				SetContent(entityString(e, "content")).
				SetColor(entityStringOr(e, "color", "#FFFFFF")).
				SetBgColor(entityStringOr(e, "bg_color", "#000000")).
				SetFontSize(entityIntOr(e, "font_size", 32)).
				Exec(ctx)
		},
	}

	// ---- pixel-art -------------------------------------------------------
	kinds[artifactKindPixelArt] = &artifactKindSpec{
		Kind:      artifactKindPixelArt,
		NameField: "name",
		Fields:    fieldSet("name", "grid_width", "grid_height", "frames", "bindings", "api_url", "enabled", "is_draft"),
		Validate: func(e map[string]any) error {
			if strings.TrimSpace(entityString(e, "name")) == "" {
				return errors.New("pixel art name is required")
			}
			gw, gh := entityIntOr(e, "grid_width", 32), entityIntOr(e, "grid_height", 32)
			if gw < 1 || gw > render.MaxPixelGrid {
				return fmt.Errorf("grid_width must be between 1 and %d", render.MaxPixelGrid)
			}
			if gh < 1 || gh > render.MaxPixelGrid {
				return fmt.Errorf("grid_height must be between 1 and %d", render.MaxPixelGrid)
			}
			frames := entityStringOr(e, "frames", "{}")
			if _, err := render.ParsePixelFrames(frames, gw, gh); err != nil {
				return err
			}
			return validPixelBindings(entityStringOr(e, "bindings", "{}"))
		},
		Requires: func(e map[string]any) []ArtifactRequirement {
			var out []ArtifactRequirement
			seen := map[string]bool{}
			var binds map[string]any
			if err := json.Unmarshal([]byte(entityStringOr(e, "bindings", "{}")), &binds); err == nil {
				collectBindingRefs(&out, seen, binds)
			}
			return out
		},
		Preview: pixelArtPreview,
		Load: func(ctx context.Context, db *ent.Client, id int) (map[string]any, error) {
			row, err := db.PixelArt.Get(ctx, id)
			if err != nil {
				return nil, err
			}
			return artifactRowMap(row)
		},
		Existing: func(ctx context.Context, db *ent.Client, name string) (int, error) {
			row, err := db.PixelArt.Query().Where(pixelart.NameEQ(name)).First(ctx)
			if err != nil {
				return 0, nil
			}
			return row.ID, nil
		},
		Create: func(ctx context.Context, tx *ent.Tx, e map[string]any, name string) (int, error) {
			row, err := tx.PixelArt.Create().
				SetName(name).
				SetGridWidth(entityIntOr(e, "grid_width", 32)).
				SetGridHeight(entityIntOr(e, "grid_height", 32)).
				SetFrames(entityStringOr(e, "frames", "{}")).
				SetBindings(entityStringOr(e, "bindings", "{}")).
				SetAPIURL(entityString(e, "api_url")).
				SetEnabled(entityBoolOr(e, "enabled", true)).
				SetIsDraft(entityBool(e, "is_draft")).
				Save(ctx)
			if err != nil {
				return 0, err
			}
			if gs, err := tx.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(ctx); err == nil && gs != nil {
				_ = tx.GeneralSettings.UpdateOne(gs).AddPixelArts(row).Exec(ctx)
			}
			return row.ID, nil
		},
		Update: func(ctx context.Context, tx *ent.Tx, id int, e map[string]any, name string) error {
			return tx.PixelArt.UpdateOneID(id).
				SetName(name).
				SetGridWidth(entityIntOr(e, "grid_width", 32)).
				SetGridHeight(entityIntOr(e, "grid_height", 32)).
				SetFrames(entityStringOr(e, "frames", "{}")).
				SetBindings(entityStringOr(e, "bindings", "{}")).
				SetAPIURL(entityString(e, "api_url")).
				SetEnabled(entityBoolOr(e, "enabled", true)).
				SetIsDraft(entityBool(e, "is_draft")).
				Exec(ctx)
		},
	}

	return kinds
}

// collectBindingRefs walks pixel-art bindings for source references.
func collectBindingRefs(out *[]ArtifactRequirement, seen map[string]bool, v any) {
	switch t := v.(type) {
	case map[string]any:
		st := entityString(t, "source_type")
		if st != "" {
			collectDatasourceRefs(out, seen, st, entityInt(t, "source_id"))
		}
		for _, val := range t {
			collectBindingRefs(out, seen, val)
		}
	case []any:
		for _, item := range t {
			collectBindingRefs(out, seen, item)
		}
	}
}

// ---------------------------------------------------------------------------
// Entity value helpers with defaults
// ---------------------------------------------------------------------------

func entityStringOr(e map[string]any, key, def string) string {
	if v := entityString(e, key); v != "" {
		return v
	}
	return def
}

func entityIntOr(e map[string]any, key string, def int) int {
	if hasEntityKey(e, key) {
		return entityInt(e, key)
	}
	return def
}

func entityFloatOr(e map[string]any, key string, def float64) float64 {
	if hasEntityKey(e, key) {
		if v := entityFloat(e, key); v != 0 {
			return v
		}
	}
	return def
}

func entityBoolOr(e map[string]any, key string, def bool) bool {
	if hasEntityKey(e, key) {
		return entityBool(e, key)
	}
	return def
}

// ---------------------------------------------------------------------------
// Previews
// ---------------------------------------------------------------------------

// themePreview renders a small three-band color swatch.
func themePreview(e map[string]any) ([]byte, error) {
	parse := func(k, def string) color.RGBA {
		raw := entityStringOr(e, k, def)
		var r, g, b uint8
		if _, err := fmt.Sscanf(raw, "#%02x%02x%02x", &r, &g, &b); err != nil {
			return color.RGBA{0, 0, 0, 255}
		}
		return color.RGBA{r, g, b, 255}
	}
	img := image.NewRGBA(image.Rect(0, 0, 96, 48))
	bands := []color.RGBA{parse("bg_color", "#282a36"), parse("accent_color", "#50fa7b"), parse("text_color", "#8be9fd")}
	for i, col := range bands {
		draw.Draw(img, image.Rect(i*32, 0, (i+1)*32, 48), &image.Uniform{col}, image.Point{}, draw.Src)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pixelArtPreview renders the first frame of a pixel art document.
func pixelArtPreview(e map[string]any) ([]byte, error) {
	gw, gh := entityIntOr(e, "grid_width", 32), entityIntOr(e, "grid_height", 32)
	doc, err := render.ParsePixelFrames(entityStringOr(e, "frames", "{}"), gw, gh)
	if err != nil {
		return nil, err
	}
	w, h := gw*4, gh*4
	if w > 256 {
		w = 256
	}
	if h > 256 {
		h = 256
	}
	img, err := render.RenderPixelFramesGrid(doc, 0, gw, gh, w, h, nil)
	if err != nil || img == nil {
		return nil, err
	}
	return img.Data, nil
}

// slugify produces a filesystem-safe filename fragment.
func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "artifact"
	}
	if len(out) > 48 {
		out = strings.Trim(out[:48], "-")
	}
	return out
}
