package adt

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ADT on 7.50 has no where-used for a table field; SAP's where-used index
// (WBCROSSGT) has it. Every name below goes into an SQL literal, so these
// patterns are the injection boundary.
var (
	fieldUsageTable = regexp.MustCompile(`^[A-Z0-9_/]{1,30}$`)
	fieldUsageField = regexp.MustCompile(`^[A-Z0-9_]{1,30}$`)
	// A class include is the class name padded with '=' to 30 characters,
	// then its part. CMnnn is the method with TMDIR.METHODINDX nnn in base 36.
	classIncludeName = regexp.MustCompile(`^(.{30})(CM[0-9A-Z]{3}|CU|CI|CO|CP|CT|CCDEF|CCIMP|CCMAC|CCAU)$`)
)

// FieldWhereUsedResult is the code SAP's where-used index lists for a table field.
type FieldWhereUsedResult struct {
	Table     string            `json:"table"`
	Field     string            `json:"field"`
	IndexRows int               `json:"index_rows"`
	Truncated bool              `json:"truncated"`
	Classes   []FieldUsageClass `json:"classes"`
	Programs  []string          `json:"programs"`
	Includes  []string          `json:"includes"`
	Other     []FieldUsageOther `json:"other"`
	Counts    FieldUsageCounts  `json:"counts"`
	Note      string            `json:"note,omitempty"`
}

// FieldUsageClass is a class whose methods or sections use the field.
type FieldUsageClass struct {
	Class   string   `json:"class"`
	Methods []string `json:"methods,omitempty"`
	Parts   []string `json:"parts,omitempty"`
}

// FieldUsageOther is an include TRDIR files as neither report nor include.
type FieldUsageOther struct {
	Name string `json:"name"`
	Subc string `json:"subc"`
}

// FieldUsageCounts sums up a FieldWhereUsedResult.
type FieldUsageCounts struct {
	Classes                 int `json:"classes"`
	Methods                 int `json:"methods"`
	Programs                int `json:"programs"`
	Includes                int `json:"includes"`
	Other                   int `json:"other"`
	UnitTestIncludesLeftOut int `json:"unit_test_includes_left_out"`
}

// FieldWhereUsed lists the classes (with methods), programs and includes that
// SAP's where-used index records for TABLE-FIELD. It reads WBCROSSGT, TMDIR and
// TRDIR through the DDIC data preview, never freestyle SQL.
func (c *Client) FieldWhereUsed(ctx context.Context, table, field string, maxRows int) (*FieldWhereUsedResult, error) {
	if err := c.checkSafety(OpRead, "FieldWhereUsed"); err != nil {
		return nil, err
	}
	table, field = strings.ToUpper(strings.TrimSpace(table)), strings.ToUpper(strings.TrimSpace(field))
	if !fieldUsageTable.MatchString(table) || !fieldUsageField.MatchString(field) {
		return nil, fmt.Errorf("give a table name and a field name, e.g. table T549A and field CALCR")
	}
	if maxRows <= 0 {
		maxRows = 1000
	}
	// Above UnlimitedRows the data preview silently returns ~100 rows, which would read as complete.
	maxRows = min(maxRows, UnlimitedRows-1)

	// The backslash is literal in ABAP SQL.
	index, err := c.GetTableContents(ctx, "WBCROSSGT", maxRows+1,
		fmt.Sprintf(`SELECT INCLUDE FROM WBCROSSGT WHERE OTYPE = 'TY' AND NAME = '%s\TY:%s'`, table, field))
	if err != nil {
		return nil, fmt.Errorf("WBCROSSGT: %w", err)
	}
	res := &FieldWhereUsedResult{Table: table, Field: field,
		Classes: []FieldUsageClass{}, Programs: []string{}, Includes: []string{}, Other: []FieldUsageOther{}}
	rows := index.Rows
	if len(rows) > maxRows {
		rows, res.Truncated = rows[:maxRows], true
	}
	res.IndexRows = len(rows)

	methods := map[string]map[int]string{} // class -> method index -> include part
	parts := map[string][]string{}
	var progs []string
	for _, r := range rows {
		inc := rowString(r, "INCLUDE")
		m := classIncludeName.FindStringSubmatch(inc)
		switch {
		case m == nil:
			progs = append(progs, inc)
		case m[2] == "CCAU":
			res.Counts.UnitTestIncludesLeftOut++
		default:
			cls := strings.TrimRight(m[1], "=")
			if methods[cls] == nil {
				methods[cls] = map[int]string{}
			}
			if strings.HasPrefix(m[2], "CM") {
				n, _ := strconv.ParseInt(m[2][2:], 36, 32) // the pattern allows only [0-9A-Z]{3}
				methods[cls][int(n)] = m[2]
			} else {
				parts[cls] = append(parts[cls], m[2])
			}
		}
	}

	// TMDIR: both IN lists bound the rows exactly (key CLASSNAME, METHODINDX).
	for batch := range slices.Chunk(sqlSafe(slices.Sorted(maps.Keys(methods))), 20) {
		idx := map[int]bool{}
		for _, cls := range batch {
			for i := range methods[cls] {
				idx[i] = true
			}
		}
		if len(idx) == 0 {
			continue
		}
		var nums []string
		for _, i := range slices.Sorted(maps.Keys(idx)) {
			nums = append(nums, strconv.Itoa(i))
		}
		got, err := c.GetTableContents(ctx, "TMDIR", len(batch)*len(nums), fmt.Sprintf(
			"SELECT CLASSNAME, METHODINDX, METHODNAME FROM TMDIR WHERE CLASSNAME IN ( %s ) AND METHODINDX IN ( %s )",
			quotedList(batch), strings.Join(nums, ", ")))
		if err != nil {
			return nil, fmt.Errorf("TMDIR: %w", err)
		}
		for _, r := range got.Rows {
			cls := rowString(r, "CLASSNAME")
			i, _ := strconv.Atoi(rowString(r, "METHODINDX"))
			if _, used := methods[cls][i]; used {
				methods[cls][i] = rowString(r, "METHODNAME")
			}
		}
	}
	for _, cls := range slices.Sorted(maps.Keys(methods)) {
		// A method TMDIR did not name keeps its include part, e.g. CM00A.
		names := slices.Sorted(maps.Values(methods[cls]))
		slices.Sort(parts[cls])
		res.Classes = append(res.Classes, FieldUsageClass{Class: cls, Methods: names, Parts: parts[cls]})
		res.Counts.Methods += len(names)
	}

	slices.Sort(progs)
	progs = slices.Compact(progs)
	subc := map[string]string{}
	for batch := range slices.Chunk(sqlSafe(progs), 20) {
		got, err := c.GetTableContents(ctx, "TRDIR", len(batch),
			fmt.Sprintf("SELECT NAME, SUBC FROM TRDIR WHERE NAME IN ( %s )", quotedList(batch)))
		if err != nil {
			return nil, fmt.Errorf("TRDIR: %w", err)
		}
		for _, r := range got.Rows {
			subc[rowString(r, "NAME")] = rowString(r, "SUBC")
		}
	}
	for _, p := range progs {
		switch subc[p] {
		case "1":
			res.Programs = append(res.Programs, p)
		case "I":
			res.Includes = append(res.Includes, p)
		default:
			res.Other = append(res.Other, FieldUsageOther{Name: p, Subc: subc[p]})
		}
	}

	res.Counts.Classes, res.Counts.Programs = len(res.Classes), len(res.Programs)
	res.Counts.Includes, res.Counts.Other = len(res.Includes), len(res.Other)
	switch {
	case res.Truncated:
		res.Note = fmt.Sprintf("WBCROSSGT has more than %d rows for %s-%s; %d of them were read, in no particular order. "+
			"Raise max_results for the rest.", maxRows, table, field, maxRows)
	case res.IndexRows == 0:
		res.Note = fmt.Sprintf("WBCROSSGT has no rows for %s-%s. The index may be incomplete "+
			"(code never re-indexed, generated code), so grep a likely package before concluding nothing uses it.",
			table, field)
	}
	return res, nil
}

// sqlSafe keeps the names that can go into an SQL literal; the rest stay unresolved.
func sqlSafe(names []string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(n string) bool { return checkSQLLiteral(n) != nil })
}

func quotedList(names []string) string {
	return "'" + strings.Join(names, "', '") + "'"
}
