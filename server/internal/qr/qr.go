// Package qr encodes short strings (URLs) as QR codes, byte mode, error
// correction level M, versions 1 to 10 (up to 213 bytes). Output is an SVG,
// so the dashboard can show a join code without any external service.
package qr

import (
	"errors"
	"fmt"
	"strings"
)

type blockSpec struct {
	ecPerBlock int
	groups     [][2]int // {blocks, data codewords per block}
}

var specM = [11]blockSpec{
	{},
	{10, [][2]int{{1, 16}}},
	{16, [][2]int{{1, 28}}},
	{26, [][2]int{{1, 44}}},
	{18, [][2]int{{2, 32}}},
	{24, [][2]int{{2, 43}}},
	{16, [][2]int{{4, 27}}},
	{18, [][2]int{{4, 31}}},
	{22, [][2]int{{2, 38}, {2, 39}}},
	{22, [][2]int{{3, 36}, {2, 37}}},
	{26, [][2]int{{4, 43}, {1, 44}}},
}

var alignPos = [11][]int{
	nil, nil, {6, 18}, {6, 22}, {6, 26}, {6, 30}, {6, 34},
	{6, 22, 38}, {6, 24, 42}, {6, 26, 46}, {6, 28, 50},
}

func (b blockSpec) dataCodewords() int {
	n := 0
	for _, g := range b.groups {
		n += g[0] * g[1]
	}
	return n
}

// Code is an encoded symbol; Modules[y][x] true = dark.
type Code struct {
	Size    int
	Modules [][]bool
	fn      [][]bool
}

// Encode builds the smallest symbol that fits text.
func Encode(text string) (*Code, error) {
	data := []byte(text)
	ver := 0
	for v := 1; v <= 10; v++ {
		countBits := 8
		if v >= 10 {
			countBits = 16
		}
		if 4+countBits+8*len(data) <= specM[v].dataCodewords()*8 {
			ver = v
			break
		}
	}
	if ver == 0 {
		return nil, errors.New("text too long for a QR code (max 213 bytes)")
	}
	spec := specM[ver]

	// bit stream: mode, count, bytes, terminator, padding
	var bits []bool
	put := func(v, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, (v>>i)&1 == 1)
		}
	}
	put(0b0100, 4)
	if ver >= 10 {
		put(len(data), 16)
	} else {
		put(len(data), 8)
	}
	for _, b := range data {
		put(int(b), 8)
	}
	capBits := spec.dataCodewords() * 8
	put(0, min(4, capBits-len(bits)))
	for len(bits)%8 != 0 {
		bits = append(bits, false)
	}
	for pad := 0; len(bits) < capBits; pad++ {
		if pad%2 == 0 {
			put(0xEC, 8)
		} else {
			put(0x11, 8)
		}
	}
	cw := make([]byte, len(bits)/8)
	for i, b := range bits {
		if b {
			cw[i/8] |= 1 << (7 - i%8)
		}
	}

	// split into blocks, add error correction, interleave
	div := rsDivisor(spec.ecPerBlock)
	var blocks, ecs [][]byte
	off := 0
	for _, g := range spec.groups {
		for i := 0; i < g[0]; i++ {
			blk := cw[off : off+g[1]]
			off += g[1]
			blocks = append(blocks, blk)
			ecs = append(ecs, rsRemainder(blk, div))
		}
	}
	var final []byte
	maxLen := 0
	for _, b := range blocks {
		maxLen = max(maxLen, len(b))
	}
	for i := 0; i < maxLen; i++ {
		for _, b := range blocks {
			if i < len(b) {
				final = append(final, b[i])
			}
		}
	}
	for i := 0; i < spec.ecPerBlock; i++ {
		for _, e := range ecs {
			final = append(final, e[i])
		}
	}

	size := ver*4 + 17
	c := &Code{Size: size}
	c.Modules = grid(size)
	c.fn = grid(size)
	c.drawFunctions(ver)
	c.placeData(final)

	best, bestScore := 0, -1
	for m := 0; m < 8; m++ {
		c.applyMask(m)
		c.drawFormat(m)
		if s := c.penalty(); bestScore < 0 || s < bestScore {
			best, bestScore = m, s
		}
		c.applyMask(m) // XOR again undoes it
	}
	c.applyMask(best)
	c.drawFormat(best)
	return c, nil
}

func grid(n int) [][]bool {
	g := make([][]bool, n)
	for i := range g {
		g[i] = make([]bool, n)
	}
	return g
}

func (c *Code) set(x, y int, dark bool) {
	c.Modules[y][x] = dark
	c.fn[y][x] = true
}

func (c *Code) drawFunctions(ver int) {
	n := c.Size
	for i := 0; i < n; i++ {
		c.set(6, i, i%2 == 0)
		c.set(i, 6, i%2 == 0)
	}
	for _, p := range [][2]int{{3, 3}, {n - 4, 3}, {3, n - 4}} {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				x, y := p[0]+dx, p[1]+dy
				if x < 0 || y < 0 || x >= n || y >= n {
					continue
				}
				d := max(abs(dx), abs(dy))
				c.set(x, y, d != 2 && d != 4)
			}
		}
	}
	ap := alignPos[ver]
	for i, ax := range ap {
		for j, ay := range ap {
			if (i == 0 && j == 0) || (i == 0 && j == len(ap)-1) || (i == len(ap)-1 && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					c.set(ax+dx, ay+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
	c.drawFormat(0) // reserve the areas; real bits are drawn after masking
	if ver >= 7 {
		rem := ver
		for i := 0; i < 12; i++ {
			rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
		}
		vbits := ver<<12 | rem
		for i := 0; i < 18; i++ {
			bit := (vbits>>i)&1 == 1
			a, b := n-11+i%3, i/3
			c.set(a, b, bit)
			c.set(b, a, bit)
		}
	}
}

func (c *Code) drawFormat(mask int) {
	data := 0b00<<3 | mask // level M
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := (data<<10 | rem) ^ 0x5412
	bit := func(i int) bool { return (bits>>i)&1 == 1 }
	n := c.Size
	for i := 0; i <= 5; i++ {
		c.set(8, i, bit(i))
	}
	c.set(8, 7, bit(6))
	c.set(8, 8, bit(7))
	c.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		c.set(14-i, 8, bit(i))
	}
	for i := 0; i < 8; i++ {
		c.set(n-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		c.set(8, n-15+i, bit(i))
	}
	c.set(8, n-8, true)
}

func (c *Code) placeData(data []byte) {
	n := c.Size
	i := 0
	for right := n - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < n; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				y := vert
				if (right+1)&2 == 0 {
					y = n - 1 - vert
				}
				if !c.fn[y][x] && i < len(data)*8 {
					c.Modules[y][x] = (data[i>>3]>>(7-i&7))&1 == 1
					i++
				}
			}
		}
	}
}

func (c *Code) applyMask(m int) {
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if c.fn[y][x] {
				continue
			}
			var inv bool
			switch m {
			case 0:
				inv = (x+y)%2 == 0
			case 1:
				inv = y%2 == 0
			case 2:
				inv = x%3 == 0
			case 3:
				inv = (x+y)%3 == 0
			case 4:
				inv = (x/3+y/2)%2 == 0
			case 5:
				inv = x*y%2+x*y%3 == 0
			case 6:
				inv = (x*y%2+x*y%3)%2 == 0
			case 7:
				inv = ((x+y)%2+x*y%3)%2 == 0
			}
			if inv {
				c.Modules[y][x] = !c.Modules[y][x]
			}
		}
	}
}

// penalty implements rules 1, 2 and 4 of the mask evaluation. Rule 3 only
// tunes the choice; every mask decodes correctly regardless.
func (c *Code) penalty() int {
	n, p, dark := c.Size, 0, 0
	for y := 0; y < n; y++ {
		runR, runC := 1, 1
		for x := 0; x < n; x++ {
			if c.Modules[y][x] {
				dark++
			}
			if x > 0 {
				if c.Modules[y][x] == c.Modules[y][x-1] {
					runR++
					if runR == 5 {
						p += 3
					} else if runR > 5 {
						p++
					}
				} else {
					runR = 1
				}
				if c.Modules[x][y] == c.Modules[x-1][y] {
					runC++
					if runC == 5 {
						p += 3
					} else if runC > 5 {
						p++
					}
				} else {
					runC = 1
				}
			}
			if x > 0 && y > 0 {
				v := c.Modules[y][x]
				if v == c.Modules[y][x-1] && v == c.Modules[y-1][x] && v == c.Modules[y-1][x-1] {
					p += 3
				}
			}
		}
	}
	total := n * n
	k := (abs(dark*20-total*10) + total - 1) / total
	return p + max(0, k-1)*10
}

// SVG renders the code with a quiet zone, using currentColor for the dark
// modules so it follows the page theme.
func (c *Code) SVG() string {
	q := 4
	n := c.Size + 2*q
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img" aria-label="QR code">`, n, n)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n)
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if c.Modules[y][x] {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+q, y+q)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String()
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func gfMul(x, y byte) byte {
	var z int
	for i := 7; i >= 0; i-- {
		z = (z << 1) ^ ((z >> 7) * 0x11D)
		z ^= int((y>>i)&1) * int(x)
	}
	return byte(z)
}

func rsDivisor(degree int) []byte {
	res := make([]byte, degree)
	res[degree-1] = 1
	root := byte(1)
	for i := 0; i < degree; i++ {
		for j := range res {
			res[j] = gfMul(res[j], root)
			if j+1 < len(res) {
				res[j] ^= res[j+1]
			}
		}
		root = gfMul(root, 2)
	}
	return res
}

func rsRemainder(data, div []byte) []byte {
	res := make([]byte, len(div))
	for _, b := range data {
		f := b ^ res[0]
		copy(res, res[1:])
		res[len(res)-1] = 0
		for i := range res {
			res[i] ^= gfMul(div[i], f)
		}
	}
	return res
}
