// Package sourcemaps lets operators upload JavaScript source maps so
// minified stack frames resolve back to the original file/line. One map per
// (project, release, bundle URL); re-uploading replaces the previous map.
//
// The mappings consumer below is deliberately hand-rolled (~150 lines of
// base64-VLQ decoding) instead of a third-party dependency: errgotrack
// ships as a single static binary and only needs the forward lookup
// (generated line/col -> original location), not the full source-map spec
// (indexed maps with "sections" are rejected as unsupported).
package sourcemaps

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/philippdormann/splat/internal/database"
)

// MaxMapBytes caps a single uploaded map at 10 MiB - large enough for real
// bundles, small enough that one upload can't blow up the database.
const MaxMapBytes = 10 << 20

var ErrTooLarge = errors.New("source map exceeds 10 MiB")
var ErrUnsupported = errors.New("unsupported source map (want version 3 with mappings)")

type Meta struct {
	ID        int64     `json:"id"`
	Release   string    `json:"release"`
	BundleURL string    `json:"bundleUrl"`
	SizeBytes int64     `json:"sizeBytes"`
	CreatedAt time.Time `json:"createdAt"`
}

// MappedLoc is the original location a minified frame maps to.
type MappedLoc struct {
	Source   string   `json:"source"`
	Line     int      `json:"line"` // 1-based, like the incoming frame
	Column   int      `json:"column"`
	Function string   `json:"function,omitempty"`
	Context  []string `json:"context,omitempty"` // source lines around Line, if sourcesContent was embedded
}

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

// Upload validates and stores a map, replacing any previous map for the
// same (project, release, bundle URL) triple.
func (s *Store) Upload(projectID int64, release, bundleURL string, content []byte) (*Meta, error) {
	bundleURL = strings.TrimSpace(bundleURL)
	if bundleURL == "" {
		return nil, errors.New("bundle URL is required")
	}
	if len(content) == 0 {
		return nil, errors.New("empty source map")
	}
	if len(content) > MaxMapBytes {
		return nil, ErrTooLarge
	}
	m, err := parseMap(content)
	if err != nil {
		return nil, err
	}
	_ = m // validated; decoded on demand at resolve time
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if s.db.IsPostgres() {
		var id int64
		err := s.db.QueryRow(`
			INSERT INTO source_maps (project_id, release, bundle_url, content, size_bytes, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (project_id, release, bundle_url)
			DO UPDATE SET content = EXCLUDED.content, size_bytes = EXCLUDED.size_bytes, created_at = EXCLUDED.created_at
			RETURNING id`,
			projectID, release, bundleURL, string(content), len(content), now).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("upsert source map: %w", err)
		}
		return s.getMeta(projectID, id)
	}
	res, err := s.db.Exec(`
		INSERT INTO source_maps (project_id, release, bundle_url, content, size_bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (project_id, release, bundle_url)
		DO UPDATE SET content = excluded.content, size_bytes = excluded.size_bytes, created_at = excluded.created_at`,
		projectID, release, bundleURL, string(content), len(content), now)
	if err != nil {
		return nil, fmt.Errorf("upsert source map: %w", err)
	}
	// On conflict LastInsertId is 0; look the row back up by its key.
	id, _ := res.LastInsertId()
	if id == 0 {
		var lookup int64
		if err := s.db.QueryRow(`SELECT id FROM source_maps WHERE project_id = ? AND release = ? AND bundle_url = ?`,
			projectID, release, bundleURL).Scan(&lookup); err != nil {
			return nil, err
		}
		id = lookup
	}
	return s.getMeta(projectID, id)
}

func (s *Store) getMeta(projectID, id int64) (*Meta, error) {
	var m Meta
	var created string
	err := s.db.QueryRow(`SELECT id, release, bundle_url, size_bytes, created_at FROM source_maps WHERE id = ? AND project_id = ?`,
		id, projectID).Scan(&m.ID, &m.Release, &m.BundleURL, &m.SizeBytes, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("source map not found")
	}
	if err != nil {
		return nil, err
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &m, nil
}

func (s *Store) List(projectID int64) ([]Meta, error) {
	rows, err := s.db.Query(`SELECT id, release, bundle_url, size_bytes, created_at FROM source_maps WHERE project_id = ? ORDER BY bundle_url, release`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Meta{}
	for rows.Next() {
		var m Meta
		var created string
		if err := rows.Scan(&m.ID, &m.Release, &m.BundleURL, &m.SizeBytes, &created); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Delete(projectID, id int64) error {
	_, err := s.db.Exec(`DELETE FROM source_maps WHERE id = ? AND project_id = ?`, id, projectID)
	return err
}

// Resolve maps a minified frame (1-based line, 0-based column as Sentry
// sends colno) to its original location. It tries maps for the exact
// release first, then maps uploaded without a release. ok=false means no
// map matched or the position is unmapped - callers show the minified
// frame unchanged in that case.
func (s *Store) Resolve(projectID int64, release, bundleFile string, line, col int) (loc MappedLoc, ok bool) {
	if line < 1 || col < 0 {
		return MappedLoc{}, false
	}
	releases := []string{release}
	if release != "" {
		releases = append(releases, "")
	}
	for _, rel := range releases {
		content, bundle, found := s.findMap(projectID, rel, bundleFile)
		if !found {
			continue
		}
		m, err := parseMap([]byte(content))
		if err != nil {
			continue
		}
		if loc, ok := m.lookup(line, col); ok {
			_ = bundle
			return loc, true
		}
	}
	return MappedLoc{}, false
}

// findMap returns the content of the best-matching map for a bundle file:
// exact URL match wins, otherwise the longest suffix match (so an upload
// recorded as "/static/app.min.js" matches a frame from
// "https://cdn.example.com/static/app.min.js?v=2" and vice versa).
func (s *Store) findMap(projectID int64, release, bundleFile string) (content, bundle string, found bool) {
	file := stripQuery(bundleFile)
	rows, err := s.db.Query(`SELECT bundle_url, content FROM source_maps WHERE project_id = ? AND release = ?`, projectID, release)
	if err != nil {
		return "", "", false
	}
	defer rows.Close()
	bestLen := -1
	for rows.Next() {
		var url, c string
		if err := rows.Scan(&url, &c); err != nil {
			continue
		}
		u := stripQuery(url)
		ml := matchLen(file, u)
		if ml > bestLen {
			bestLen, content, bundle, found = ml, c, url, true
		}
	}
	return content, bundle, found
}

func stripQuery(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		return u[:i]
	}
	return u
}

// matchLen returns the match quality: len+1 for an exact match, the shared
// suffix length for a suffix match, -1 for no match.
func matchLen(a, b string) int {
	if a == b {
		return len(a) + 1
	}
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	// The shared suffix must start on a path boundary - either string
	// exhausted, or the char before the suffix is '/' - so
	// "xapp.min.js" doesn't match "app.min.js" while "/static/app.min.js"
	// matches "...com/static/app.min.js".
	if n > 0 && (n == len(a) || n == len(b) || a[len(a)-1-n] == '/' || b[len(b)-1-n] == '/') {
		return n
	}
	return -1
}

// --- minimal source-map consumer ---

type rawMap struct {
	Version        int      `json:"version"`
	Sources        []string `json:"sources"`
	SourcesContent []string `json:"sourcesContent"`
	Names          []string `json:"names"`
	Mappings       string   `json:"mappings"`
}

type seg struct {
	genCol  int
	srcIdx  int
	srcLine int // 0-based
	srcCol  int
	nameIdx int // -1 when the segment carries no name
}

type consumer struct {
	sources  []string
	contents []string
	names    []string
	lines    [][]seg // per generated line (0-based), sorted by genCol
}

func parseMap(content []byte) (*consumer, error) {
	var raw rawMap
	dec := json.NewDecoder(strings.NewReader(string(content)))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("invalid source map JSON: %w", err)
	}
	if raw.Version != 3 || raw.Mappings == "" {
		return nil, ErrUnsupported
	}
	c := &consumer{sources: raw.Sources, contents: raw.SourcesContent, names: raw.Names}
	srcIdx, srcLine, srcCol, nameIdx := 0, 0, 0, 0
	for _, line := range strings.Split(raw.Mappings, ";") {
		var segs []seg
		genCol := 0
		if line == "" {
			c.lines = append(c.lines, nil)
			continue
		}
		for _, field := range strings.Split(line, ",") {
			nums, err := decodeVLQ(field)
			if err != nil || len(nums) < 1 {
				return nil, fmt.Errorf("invalid mappings segment %q", field)
			}
			genCol += nums[0]
			s := seg{genCol: genCol, srcIdx: -1, nameIdx: -1}
			if len(nums) >= 4 {
				srcIdx += nums[1]
				srcLine += nums[2]
				srcCol += nums[3]
				s.srcIdx, s.srcLine, s.srcCol = srcIdx, srcLine, srcCol
				if len(nums) >= 5 {
					nameIdx += nums[4]
					s.nameIdx = nameIdx
				}
			}
			segs = append(segs, s)
		}
		c.lines = append(c.lines, segs)
	}
	return c, nil
}

// lookup resolves a 1-based line and 0-based column to the segment with the
// greatest generated column at or before it (mappings anchor the start of
// generated ranges, so a position inside minified code belongs to the
// segment that starts nearest before it).
func (c *consumer) lookup(line, col int) (MappedLoc, bool) {
	if line < 1 || line > len(c.lines) {
		return MappedLoc{}, false
	}
	segs := c.lines[line-1]
	best := -1
	for i, s := range segs {
		if s.genCol > col {
			break
		}
		if s.srcIdx >= 0 {
			best = i
		}
	}
	if best < 0 {
		return MappedLoc{}, false
	}
	s := segs[best]
	if s.srcIdx >= len(c.sources) {
		return MappedLoc{}, false
	}
	loc := MappedLoc{Source: c.sources[s.srcIdx], Line: s.srcLine + 1, Column: s.srcCol}
	if s.nameIdx >= 0 && s.nameIdx < len(c.names) {
		loc.Function = c.names[s.nameIdx]
	}
	if s.srcIdx < len(c.contents) && c.contents[s.srcIdx] != "" {
		lines := strings.Split(c.contents[s.srcIdx], "\n")
		// 1-based loc.Line with two lines of context each side.
		for i := loc.Line - 3; i <= loc.Line+1; i++ {
			if i >= 0 && i < len(lines) {
				loc.Context = append(loc.Context, lines[i])
			}
		}
	}
	return loc, true
}

const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

func b64val(c byte) (int, bool) {
	i := strings.IndexByte(b64, c)
	return i, i >= 0
}

// decodeVLQ decodes one base64-VLQ field into its signed values.
func decodeVLQ(field string) ([]int, error) {
	out := []int{}
	vals := 0
	shift := 0
	for i := 0; i < len(field); i++ {
		d, ok := b64val(field[i])
		if !ok {
			return nil, fmt.Errorf("bad base64 char %q", field[i])
		}
		cont := d & 32
		d &= 31
		vals |= d << shift
		shift += 5
		if cont == 0 {
			neg := vals&1 != 0
			vals >>= 1
			if neg {
				out = append(out, -vals)
			} else {
				out = append(out, vals)
			}
			vals, shift = 0, 0
		}
	}
	if shift != 0 {
		return nil, fmt.Errorf("truncated VLQ field %q", field)
	}
	return out, nil
}
