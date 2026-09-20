// formats.go CSV / XLSX / SPSS .sav 三种落盘格式
package export

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// CSV UTF-8 + BOM（Excel 直开不乱码）
func (m *Matrix) CSV() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&buf)
	if err := w.Write(m.Headers); err != nil {
		return nil, err
	}
	for _, r := range m.Rows {
		if err := w.Write(r); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// XLSX 两个工作表：答卷明细 + 结果码分布
func (m *Matrix) XLSX() ([]byte, error) {
	f := excelize.NewFile()
	s1 := "答卷明细"
	if _, err := f.NewSheet(s1); err != nil {
		return nil, err
	}
	_ = f.DeleteSheet("Sheet1")
	for i, h := range m.Headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(s1, cell, h)
	}
	for r, row := range m.Rows {
		for i, v := range row {
			cell, _ := excelize.CoordinatesToCellName(i+1, r+2)
			_ = f.SetCellValue(s1, cell, v)
		}
	}
	// 结果码分布
	s2 := "结果码分布"
	_, _ = f.NewSheet(s2)
	_ = f.SetCellValue(s2, "A1", "结果码")
	_ = f.SetCellValue(s2, "B1", "份数")
	rcCol := -1
	for i, h := range m.Headers {
		if h == "结果码" {
			rcCol = i
			break
		}
	}
	dist := map[string]int{}
	var order []string
	for _, row := range m.Rows {
		rc := ""
		if rcCol >= 0 && rcCol < len(row) {
			rc = row[rcCol]
		}
		if _, ok := dist[rc]; !ok {
			order = append(order, rc)
		}
		dist[rc]++
	}
	for i, rc := range order {
		_ = f.SetCellValue(s2, fmt.Sprintf("A%d", i+2), rc)
		_ = f.SetCellValue(s2, fmt.Sprintf("B%d", i+2), dist[rc])
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ── SPSS .sav（PSPP 兼容系统文件，未压缩；UTF-8 + 长变量名记录）──────────────

type savVar struct {
	short string // ≤8 字节短名
	label string // 题干（变量标签）
	strW  int    // 0=数值；>0=字符串宽度（字节）
}

func i32(b []byte, v int32)   { binary.LittleEndian.PutUint32(b, uint32(v)) }
func f64(b []byte, v float64) { binary.LittleEndian.PutUint64(b, math.Float64bits(v)) }

func pad4(n int) int { return (n + 3) &^ 3 }

// sanitizeLongName 长变量名清洗（SPSS 长名规则：字母数字._ ，不超 64）
func sanitizeLongName(h string, i int) string {
	var b strings.Builder
	for _, r := range h {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '.' {
			b.WriteRune(r)
		} else if r > 127 {
			fmt.Fprintf(&b, "u%X", r)
		}
	}
	name := b.String()
	if name == "" {
		name = fmt.Sprintf("col%d", i+1)
	}
	if len(name) > 60 {
		name = name[:60]
	}
	return fmt.Sprintf("c%d_%s", i+1, name)
}

// SAV 生成 SPSS 系统文件（数值题=数值变量；其余=字符串变量）
func (m *Matrix) SAV() ([]byte, error) {
	// 列 → 变量：Types[i]=1 数值变量（F8.2），否则宽 32 字符串
	vars := make([]savVar, 0, len(m.Headers))
	for i := range m.Headers {
		strW := 32
		if i < len(m.Types) && m.Types[i] == 1 {
			strW = 0
		}
		vars = append(vars, savVar{short: fmt.Sprintf("v%03d", i+1), label: m.Headers[i], strW: strW})
	}

	var out bytes.Buffer
	hdr := make([]byte, 116) // $FL2(4)+5×i32+8+9+8+64+3 = 116
	copy(hdr[0:4], "$FL2")
	i32(hdr[4:8], 2)                    // layout
	i32(hdr[8:12], int32(len(vars)))    // nominal case size
	i32(hdr[12:16], 0)                  // compression=未压缩
	i32(hdr[16:20], 0)                  // weight
	i32(hdr[20:24], int32(len(m.Rows))) // ncases
	f64(hdr[24:32], 100.0)              // bias
	now := time.Now()
	copy(hdr[32:41], now.Format("02 Jan 06"))
	copy(hdr[41:49], now.Format("15:04:05"))
	copy(hdr[49:113], "NK3C ITACATI export")
	out.Write(hdr)

	// 变量记录
	for _, v := range vars {
		rec := bytes.NewBuffer(nil)
		tmp := make([]byte, 4)
		i32(tmp, 2)
		rec.Write(tmp)
		i32(tmp, int32(v.strW)) // type
		rec.Write(tmp)
		// 标签
		label := []byte(v.label)
		i32(tmp, 1)
		rec.Write(tmp) // has_var_label=1
		i32(tmp, 0)
		rec.Write(tmp) // no missing
		// print/write 格式：数值 F8.2；字符串 A<w>
		var fmtI int32
		if v.strW == 0 {
			fmtI = 5 | 8<<8 | 2<<16
		} else {
			fmtI = 1 | int32(v.strW)<<8
		}
		i32(tmp, fmtI)
		rec.Write(tmp)
		rec.Write(tmp)
		name := make([]byte, 8)
		copy(name, v.short)
		rec.Write(name)
		i32(tmp, int32(len(label)))
		rec.Write(tmp)
		lb := make([]byte, pad4(len(label)))
		copy(lb, label)
		rec.Write(lb)
		out.Write(rec.Bytes())
	}
	// 长变量名（record 7 / subtype 13）与 UTF-8 声明（record 7 / subtype 20）
	longNames := bytes.NewBuffer(nil)
	for i, h := range m.Headers {
		fmt.Fprintf(longNames, "v%03d=%s\n", i+1, sanitizeLongName(h, i))
	}
	writeRec7 := func(subtype int32, size int32, payload []byte) {
		tmp := make([]byte, 16) // rec_type(4)+subtype(4)+size(4)+count(4)
		i32(tmp[0:4], 7)
		i32(tmp[4:8], subtype)
		i32(tmp[8:12], size)
		i32(tmp[12:16], int32(len(payload))/size)
		out.Write(tmp)
		out.Write(payload)
	}
	longRec := longNames.Bytes()
	longRec = append(longRec, make([]byte, pad4(len(longRec))-len(longRec))...)
	writeRec7(13, 1, longRec)
	writeRec7(20, 4, []byte{650 & 0xFF, 650 >> 8, 0, 0}) // 650 = UTF-8
	// 字典终止
	term := make([]byte, 8)
	i32(term[0:4], 999)
	out.Write(term)
	// 数据（未压缩）
	caseBuf := bytes.NewBuffer(nil)
	tmp := make([]byte, 8)
	for _, row := range m.Rows {
		for i, v := range vars {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			if v.strW == 0 {
				var fv float64
				fmt.Sscanf(val, "%g", &fv)
				f64(tmp, fv)
				caseBuf.Write(tmp)
			} else {
				b := make([]byte, v.strW)
				copy(b, val)
				caseBuf.Write(b)
			}
		}
	}
	out.Write(caseBuf.Bytes())
	return out.Bytes(), nil
}
