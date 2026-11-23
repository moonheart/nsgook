package devviewers

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

var model3dExts = map[string]bool{".obj": true, ".stl": true, ".ply": true, ".off": true}

// Vec3 represents a 3D point or normal vector.
type Vec3 struct{ X, Y, Z float64 }

// Tri stores vertex indices for a single triangle face.
type Tri struct{ A, B, C int }

// Mesh holds parsed geometry normalized around origin.
type Mesh struct {
	Format   string
	Vertices []Vec3
	Faces    []Tri
}

// Model3DPlugin previews OBJ, STL, PLY, and OFF 3D meshes in an interactive orbit canvas.
type Model3DPlugin struct{}

func (Model3DPlugin) ID() string              { return "model3dviewer" }
func (Model3DPlugin) Name() string            { return "Model3DViewer" }
func (Model3DPlugin) Priority() int           { return 82 }
func (Model3DPlugin) CanHandle(p string) bool { return hasExt(p, model3dExts) }

func (Model3DPlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 780, Height: 620}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (Model3DPlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)

	meshSig := core.NewSignal[*Mesh](cx, nil)
	loadErr := core.NewSignal(cx, "")
	wireframe := core.NewSignal(cx, false)
	yaw := core.NewSignal(cx, 0.55)
	pitch := core.NewSignal(cx, -0.35)
	zoom := core.NewSignal(cx, 1.0)

	var stopped atomic.Bool
	cx.Cleanup(func() {
		stopped.Store(true)
		meshSig.Set(nil)
	})

	ctx.IsBusy.Set(true)
	go func() {
		raw, err := os.ReadFile(path)
		var m *Mesh
		if err == nil {
			m, err = ParseMeshBytes(filepath.Ext(path), raw)
		}
		if stopped.Load() {
			return
		}
		cx.Post(func() {
			if stopped.Load() {
				return
			}
			ctx.IsBusy.Set(false)
			if err != nil {
				loadErr.Set(err.Error())
				return
			}
			meshSig.Set(m)
		})
	}()

	resetView := func() {
		yaw.Set(0.55)
		pitch.Set(-0.35)
		zoom.Set(1.0)
	}

	ctx.MoreItems.Set([]viewer.MenuAction{
		{
			ID:       "model-wireframe",
			Label:    tr.ModelWireframe(),
			Icon:     lucide.Box,
			Checked:  wireframe.Get,
			OnSelect: func() { wireframe.Set(!wireframe.GetUntracked()) },
		},
		{
			ID:       "model-reset",
			Label:    tr.ModelResetView(),
			Icon:     lucide.RotateCcw,
			OnSelect: resetView,
		},
	})

	var startYaw, startPitch float64
	canvas := core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Width: core.Pct(100), Height: core.Pct(100), Cursor: core.CursorPointer},
		OnPan: func(ev core.PanEvent) {
			if ev.Phase == core.GestureBegan {
				startYaw = yaw.GetUntracked()
				startPitch = pitch.GetUntracked()
			}
			yaw.Set(startYaw + float64(ev.Translation.X)*0.012)
			pitch.Set(max(-1.45, min(1.45, startPitch+float64(ev.Translation.Y)*0.012)))
		},
		OnWheel: func(ev core.WheelEvent) {
			zoom.Update(func(z float64) float64 {
				nz := z * (1 - float64(ev.Delta.Y)*0.0015)
				return max(0.25, min(6.0, nz))
			})
		},
		Children: []core.Node{
			core.Canvas{
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
				Draw: func(d *core.Drawing, size core.Size) {
					m := meshSig.Get()
					if m == nil || len(m.Faces) == 0 {
						return
					}
					w, h := float64(size.Width), float64(size.Height)
					scale := min(w, h) * 0.36 * zoom.Get()
					cx0, cy0 := w*0.5, h*0.5
					sy, cy := math.Sin(yaw.Get()), math.Cos(yaw.Get())
					sp, cp := math.Sin(pitch.Get()), math.Cos(pitch.Get())

					pts := make([]Vec3, len(m.Vertices))
					for i, v := range m.Vertices {
						x1 := v.X*cy + v.Z*sy
						z1 := -v.X*sy + v.Z*cy
						y2 := v.Y*cp - z1*sp
						z2 := v.Y*sp + z1*cp
						pts[i] = Vec3{X: x1, Y: y2, Z: z2}
					}

					type drawTri struct {
						p0, p1, p2 Vec3
						z          float64
						shade      float64
					}
					step := 1
					if len(m.Faces) > 12000 {
						step = len(m.Faces) / 12000
					}
					tris := make([]drawTri, 0, len(m.Faces)/step+1)
					for idx := 0; idx < len(m.Faces); idx += step {
						f := m.Faces[idx]
						a, b, c := pts[f.A], pts[f.B], pts[f.C]
						ux, uy, uz := b.X-a.X, b.Y-a.Y, b.Z-a.Z
						vx, vy, vz := c.X-a.X, c.Y-a.Y, c.Z-a.Z
						nx := uy*vz - uz*vy
						ny := uz*vx - ux*vz
						nz := ux*vy - uy*vx
						nlen := math.Sqrt(nx*nx + ny*ny + nz*nz)
						shade := 0.55
						if nlen > 1e-9 {
							nx, ny, nz = nx/nlen, ny/nlen, nz/nlen
							// Light vector from upper-front-right.
							dot := math.Abs(nx*0.35 + ny*(-0.55) + nz*0.75)
							shade = 0.28 + 0.72*min(1.0, dot)
						}
						tris = append(tris, drawTri{p0: a, p1: b, p2: c, z: (a.Z + b.Z + c.Z) / 3, shade: shade})
					}

					sort.Slice(tris, func(i, j int) bool { return tris[i].z < tris[j].z })

					wf := wireframe.Get()
					baseAccent := colors.Accent()
					rBase, gBase, bBase, _ := baseAccent.Components()
					edgeCol := ui.Alpha(colors.Text(), 0.35)
					if wf {
						edgeCol = baseAccent
					}
					d.SetStrokeColor(edgeCol)
					d.SetLineWidth(1)

					proj := func(v Vec3) (float32, float32) {
						persp := 3.2 / max(0.4, 3.2-v.Z*0.65)
						return float32(cx0 + v.X*scale*persp), float32(cy0 - v.Y*scale*persp)
					}

					for _, t := range tris {
						x0, y0 := proj(t.p0)
						x1, y1 := proj(t.p1)
						x2, y2 := proj(t.p2)
						d.BeginPath()
						d.MoveTo(x0, y0)
						d.LineTo(x1, y1)
						d.LineTo(x2, y2)
						d.ClosePath()
						if !wf {
							r8 := uint32(max(20.0, min(250.0, rBase*255.0*t.shade)))
							g8 := uint32(max(20.0, min(250.0, gBase*255.0*t.shade)))
							b8 := uint32(max(20.0, min(250.0, bBase*255.0*t.shade)))
							d.SetFillColor(core.RGB((r8 << 16) | (g8 << 8) | b8))
							d.Fill()
						} else {
							d.Stroke()
						}
					}
				},
			},
		},
	}

	toolbar := pluginui.Toolbar(cx,
		ui.Button{
			Text:    locales.For(cx).ModelShaded(),
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			OnPress: func() { wireframe.Set(false) },
		},
		ui.Button{
			Text:    locales.For(cx).ModelWireframe(),
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			OnPress: func() { wireframe.Set(true) },
		},
		pluginui.IconButton{Icon: lucide.RotateCcw, Label: locales.For(cx).ModelResetView(), Size: 26, OnPress: resetView},
		ui.Spacer{},
		core.Text{
			ContentFunc: func() string {
				m := meshSig.Get()
				if m == nil {
					return ""
				}
				return locales.For(cx).ModelStats(strconv.Itoa(len(m.Vertices)), strconv.Itoa(len(m.Faces)))
			},
			Style:     core.Style{FontSize: 11},
			StyleFunc: ui.Colored(colors.Muted),
		},
	)

	return pluginui.Fill(
		core.Show{
			When: func() bool { return loadErr.Get() != "" },
			Then: func(cx *core.Scope) core.Node {
				return pluginui.EmptyState(cx, lucide.FileExclamationPoint, locales.For(cx).CommonError(), loadErr.Get())
			},
			Else: func(*core.Scope) core.Node {
				return pluginui.Fill(toolbar, canvas)
			},
		},
	)
}

// ParseMeshBytes parses OBJ, ASCII/Binary STL, ASCII PLY, or OFF files and normalizes them into unit bounds.
func ParseMeshBytes(ext string, raw []byte) (*Mesh, error) {
	var m *Mesh
	var err error
	switch strings.ToLower(ext) {
	case ".obj":
		m, err = parseOBJ(raw)
	case ".stl":
		m, err = parseSTL(raw)
	case ".ply":
		m, err = parsePLY(raw)
	case ".off":
		m, err = parseOFF(raw)
	default:
		return nil, fmt.Errorf("unsupported mesh format %q", ext)
	}
	if err != nil {
		return nil, err
	}
	normalizeMesh(m)
	return m, nil
}

func normalizeMesh(m *Mesh) {
	if len(m.Vertices) == 0 {
		return
	}
	minV, maxV := m.Vertices[0], m.Vertices[0]
	for _, v := range m.Vertices[1:] {
		minV.X, maxV.X = min(minV.X, v.X), max(maxV.X, v.X)
		minV.Y, maxV.Y = min(minV.Y, v.Y), max(maxV.Y, v.Y)
		minV.Z, maxV.Z = min(minV.Z, v.Z), max(maxV.Z, v.Z)
	}
	mid := Vec3{(minV.X + maxV.X) / 2, (minV.Y + maxV.Y) / 2, (minV.Z + maxV.Z) / 2}
	span := max(maxV.X-minV.X, max(maxV.Y-minV.Y, maxV.Z-minV.Z))
	if span <= 1e-9 {
		span = 1
	}
	inv := 2.0 / span
	for i, v := range m.Vertices {
		m.Vertices[i] = Vec3{(v.X - mid.X) * inv, (v.Y - mid.Y) * inv, (v.Z - mid.Z) * inv}
	}
}

func parseOBJ(raw []byte) (*Mesh, error) {
	m := &Mesh{Format: "OBJ"}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		fields := strings.Fields(strings.TrimSpace(sc.Text()))
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "v":
			if len(fields) >= 4 {
				x, _ := strconv.ParseFloat(fields[1], 64)
				y, _ := strconv.ParseFloat(fields[2], 64)
				z, _ := strconv.ParseFloat(fields[3], 64)
				m.Vertices = append(m.Vertices, Vec3{x, y, z})
			}
		case "f":
			if len(fields) >= 4 {
				var idx []int
				for _, tok := range fields[1:] {
					vPart := strings.Split(tok, "/")[0]
					if n, err := strconv.Atoi(vPart); err == nil {
						if n < 0 {
							n = len(m.Vertices) + n
						} else {
							n--
						}
						if n >= 0 && n < len(m.Vertices) {
							idx = append(idx, n)
						}
					}
				}
				for k := 1; k+1 < len(idx); k++ {
					m.Faces = append(m.Faces, Tri{idx[0], idx[k], idx[k+1]})
				}
			}
		}
	}
	if len(m.Vertices) == 0 {
		return nil, fmt.Errorf("OBJ file contains no vertices")
	}
	return m, nil
}

func parseSTL(raw []byte) (*Mesh, error) {
	m := &Mesh{Format: "STL"}
	trimmed := bytes.TrimSpace(raw)
	if bytes.HasPrefix(trimmed, []byte("solid")) && bytes.Contains(trimmed, []byte("facet")) {
		sc := bufio.NewScanner(bytes.NewReader(raw))
		var cur []int
		for sc.Scan() {
			fields := strings.Fields(strings.TrimSpace(sc.Text()))
			if len(fields) >= 4 && fields[0] == "vertex" {
				x, _ := strconv.ParseFloat(fields[1], 64)
				y, _ := strconv.ParseFloat(fields[2], 64)
				z, _ := strconv.ParseFloat(fields[3], 64)
				cur = append(cur, len(m.Vertices))
				m.Vertices = append(m.Vertices, Vec3{x, y, z})
				if len(cur) == 3 {
					m.Faces = append(m.Faces, Tri{cur[0], cur[1], cur[2]})
					cur = cur[:0]
				}
			}
		}
		if len(m.Faces) > 0 {
			return m, nil
		}
	}

	if len(raw) < 84 {
		return nil, io.ErrUnexpectedEOF
	}
	triCount := int(binary.LittleEndian.Uint32(raw[80:84]))
	pos := 84
	for i := 0; i < triCount && pos+50 <= len(raw); i++ {
		base := len(m.Vertices)
		for k := 0; k < 3; k++ {
			voff := pos + 12 + k*12
			x := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[voff : voff+4])))
			y := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[voff+4 : voff+8])))
			z := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[voff+8 : voff+12])))
			m.Vertices = append(m.Vertices, Vec3{x, y, z})
		}
		m.Faces = append(m.Faces, Tri{base, base + 1, base + 2})
		pos += 50
	}
	return m, nil
}

func parsePLY(raw []byte) (*Mesh, error) {
	m := &Mesh{Format: "PLY"}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	var vCount, fCount int
	inBody := false
	isASCII := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !inBody {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			if fields[0] == "format" && len(fields) >= 2 && fields[1] != "ascii" {
				isASCII = false
			}
			if fields[0] == "element" && len(fields) >= 3 {
				n, _ := strconv.Atoi(fields[2])
				if fields[1] == "vertex" {
					vCount = n
				} else if fields[1] == "face" {
					fCount = n
				}
			}
			if fields[0] == "end_header" {
				inBody = true
				if !isASCII {
					return nil, fmt.Errorf("binary PLY header parsed (%d vertices, %d faces)", vCount, fCount)
				}
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if len(m.Vertices) < vCount {
			x, _ := strconv.ParseFloat(fields[0], 64)
			y, _ := strconv.ParseFloat(fields[1], 64)
			z, _ := strconv.ParseFloat(fields[2], 64)
			m.Vertices = append(m.Vertices, Vec3{x, y, z})
		} else if len(m.Faces) < fCount {
			cnt, _ := strconv.Atoi(fields[0])
			if cnt >= 3 && len(fields) >= cnt+1 {
				var idx []int
				for k := 1; k <= cnt; k++ {
					v, _ := strconv.Atoi(fields[k])
					if v >= 0 && v < len(m.Vertices) {
						idx = append(idx, v)
					}
				}
				for k := 1; k+1 < len(idx); k++ {
					m.Faces = append(m.Faces, Tri{idx[0], idx[k], idx[k+1]})
				}
			}
		}
	}
	return m, nil
}

func parseOFF(raw []byte) (*Mesh, error) {
	m := &Mesh{Format: "OFF"}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	var vCount, fCount int
	gotCounts := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || line == "OFF" {
			continue
		}
		fields := strings.Fields(line)
		if !gotCounts {
			if len(fields) >= 2 {
				vCount, _ = strconv.Atoi(fields[0])
				fCount, _ = strconv.Atoi(fields[1])
				gotCounts = true
			}
			continue
		}
		if len(m.Vertices) < vCount && len(fields) >= 3 {
			x, _ := strconv.ParseFloat(fields[0], 64)
			y, _ := strconv.ParseFloat(fields[1], 64)
			z, _ := strconv.ParseFloat(fields[2], 64)
			m.Vertices = append(m.Vertices, Vec3{x, y, z})
		} else if len(m.Faces) < fCount && len(fields) >= 4 {
			cnt, _ := strconv.Atoi(fields[0])
			if cnt >= 3 && len(fields) >= cnt+1 {
				var idx []int
				for k := 1; k <= cnt; k++ {
					v, _ := strconv.Atoi(fields[k])
					if v >= 0 && v < len(m.Vertices) {
						idx = append(idx, v)
					}
				}
				for k := 1; k+1 < len(idx); k++ {
					m.Faces = append(m.Faces, Tri{idx[0], idx[k], idx[k+1]})
				}
			}
		}
	}
	return m, nil
}
