// Copyright 2015 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package table export the table that contains the results of a BQL query.
package table

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/google/badwolf/triple/literal"
	"github.com/google/badwolf/triple/node"
	"github.com/google/badwolf/triple/predicate"
)

// Table contains the results of a BQL query. This table implementation is not
// safe for concurrency. You should take appropriate precautions if you want to
// access it concurrently and wrap to properly control concurrent operations.
type Table struct {
	bs   []string
	mbs  map[string]bool
	data []Row
}

// New returns a new table that can hold data for the the given bindings. The,
// table creation will fail if there are repeated bindings.
func New(bs []string) (*Table, error) {
	m := make(map[string]bool)
	for _, b := range bs {
		m[b] = true
	}
	if len(m) != len(bs) {
		return nil, fmt.Errorf("table.New does not allow duplicated bindings in %s", bs)
	}
	return &Table{
		bs:  bs,
		mbs: m,
	}, nil
}

// Cell contains one of the possible values that form rows.
type Cell struct {
	S *string
	N *node.Node
	P *predicate.Predicate
	L *literal.Literal
	T *time.Time
}

// String returns a readable representation of a cell.
func (c *Cell) String() string {
	if c.S != nil {
		return *c.S
	}
	if c.N != nil {
		return c.N.String()
	}
	if c.P != nil {
		return c.P.String()
	}
	if c.L != nil {
		return c.L.String()
	}
	if c.T != nil {
		return c.T.Format(time.RFC3339Nano)
	}
	return "<NULL>"
}

// Row represents a collection of cells.
type Row map[string]*Cell

// ToTextLine converts a row into line of text. To do so, it requires the list
// of bindings of the table, and the separator you want to use. If the separator
// is empty tabs will be used.
func (r Row) ToTextLine(res *bytes.Buffer, bs []string, sep string) error {
	cnt := len(bs)
	if sep == "" {
		sep = "\t"
	}
	for _, b := range bs {
		cnt--
		v := "<NULL>"
		if c, ok := r[b]; ok {
			v = c.String()
		}
		if _, err := res.WriteString(v); err != nil {
			return err
		}
		if cnt > 0 {
			res.WriteString(sep)
		}
	}
	return nil
}

// AddRow adds a row to the end of a table. For performance reasons, it does not
// check that all bindings are set, nor that they are declared on table
// creation. BQL builds valid tables, if you plan to create tables on your own
// you should be careful to provide valid rows.
func (t *Table) AddRow(r Row) {
	if len(r) > 0 {
		t.data = append(t.data, r)
	}
}

// NumRows returns the number of rows currently available on the table.
func (t *Table) NumRows() int {
	return len(t.data)
}

// Row returns the requested row. Rows start at 0. Also, if you request a row
// beyond it will return nil, and the ok boolean will be false.
func (t *Table) Row(i int) (Row, bool) {
	if i < 0 || i >= len(t.data) {
		return nil, false
	}
	return t.data[i], true
}

// Rows returns all the available rows.
func (t *Table) Rows() []Row {
	return t.data
}

// AddBindings add the new bindings provided to the table.
func (t *Table) AddBindings(bs []string) {
	for _, b := range bs {
		if !t.mbs[b] {
			t.mbs[b] = true
			t.bs = append(t.bs, b)
		}
	}
}

// ProjectBindings replaces the current bindings with the projected one. The
// provided bindings needs to be a subset of the original bindings. If the
// provided bindings are not a subset of the original ones, the projection will
// fail, leave the table unmodified, and return an error. The projection only
// modify the bindings, but does not drop non projected data.
func (t *Table) ProjectBindings(bs []string) error {
	if len(t.data) == 0 || len(t.mbs) == 0 {
		return nil
	}
	for _, b := range bs {
		if !t.mbs[b] {
			return fmt.Errorf("cannot project against unknow binding %s; known bindinds are %v", b, t.bs)
		}
	}
	t.bs = []string{}
	t.mbs = make(map[string]bool)
	t.AddBindings(bs)
	return nil
}

// HasBinding returns true if the binding currently exist on the table.
func (t *Table) HasBinding(b string) bool {
	return t.mbs[b]
}

// Bindings returns the bindings contained on the tables.
func (t *Table) Bindings() []string {
	return t.bs
}

// ToText convert the table into a readable text versions. It requires the
// separator to be used between cells.
func (t *Table) ToText(sep string) (*bytes.Buffer, error) {
	res, row := &bytes.Buffer{}, &bytes.Buffer{}
	res.WriteString(strings.Join(t.bs, sep))
	res.WriteString("\n")
	for _, r := range t.data {
		err := r.ToTextLine(row, t.bs, sep)
		if err != nil {
			return nil, err
		}
		if _, err := res.Write(row.Bytes()); err != nil {
			return nil, err
		}
		if _, err := res.WriteString("\n"); err != nil {
			return nil, err
		}
		row.Reset()
	}
	return res, nil
}

// String attempts to force serialize the table into a string.
func (t *Table) String() string {
	b, err := t.ToText("\t")
	if err != nil {
		return fmt.Sprintf("Failed to serialize to text! Error: %s", err)
	}
	return b.String()
}

// equalBindings returns true if the bindings are the same, false otherwise.
func equalBindings(b1, b2 map[string]bool) bool {
	if len(b1) != len(b2) {
		return false
	}
	for k := range b1 {
		if !b2[k] {
			return false
		}
	}
	return true
}

// AppendTable appends the content of the provided table. It will fail it the
// target table is not empty and the bindings do not match.
func (t *Table) AppendTable(t2 *Table) error {
	if t2 == nil {
		return nil
	}
	if len(t.Bindings()) > 0 && !equalBindings(t.mbs, t2.mbs) {
		return fmt.Errorf("AppendTable can only append to an empty table or equally binded table; intead got %v and %v", t.bs, t2.bs)
	}
	if len(t.Bindings()) == 0 {
		t.bs, t.mbs = t2.bs, t2.mbs
	}
	t.data = append(t.data, t2.data...)
	return nil
}

// disjointBinding returns true if they are not overlapping bindings, false
// otherwise.
func disjointBinding(b1, b2 map[string]bool) bool {
	for k := range b1 {
		if b2[k] {
			return false
		}
	}
	return true
}

// MergeRows takes a list of rows and returns a new map containing both.
func MergeRows(ms []Row) Row {
	res := make(map[string]*Cell)
	for _, om := range ms {
		for k, v := range om {
			res[k] = v
		}
	}
	return res
}

// DotProduct does the dot product with the provided table
func (t *Table) DotProduct(t2 *Table) error {
	if !disjointBinding(t.mbs, t2.mbs) {
		return fmt.Errorf("DotProduct operations requires disjoint bindingts; instead got %v and %v", t.mbs, t2.mbs)
	}
	// Update the table metadata.
	m := make(map[string]bool)
	for k := range t.mbs {
		m[k] = true
	}
	for k := range t2.mbs {
		m[k] = true
	}
	t.mbs = m
	t.bs = []string{}
	for k := range t.mbs {
		t.bs = append(t.bs, k)
	}
	// Update the data.
	td := t.data
	cnt, size := 0, len(td)*len(t2.data)
	t.data = make([]Row, size, size) // Preallocate resulting table.
	for _, r1 := range td {
		for _, r2 := range t2.data {
			t.data[cnt] = MergeRows([]Row{r1, r2})
			cnt++
		}
	}
	return nil
}

// DeleteRow removes the row at position i from the table. This should be used
// carefully. If you are planning to delete a large volume of rows consider
// creating a new table and just copy the rows you need. This operation relies
// on slices and it *will* *not* release the underlying deleted row. Please,
// see https://blog.golang.org/go-slices-usage-and-internals for a detailed
// explanation.
func (t *Table) DeleteRow(i int) error {
	if i < 0 || i >= len(t.data) {
		return fmt.Errorf("cannot delete row %d from a table with %d rows", i, len(t.data))
	}
	t.data = append(t.data[:i], t.data[i+1:]...)
	return nil
}

// Truncate flushes all the data away. It still retains all set bindings.
func (t *Table) Truncate() {
	t.data = nil
}

// Limit keeps the initial ith rows.
func (t *Table) Limit(i int64) {
	if int64(len(t.data)) > i {
		td := make([]Row, i, i) // Preallocate resulting table.
		copy(td, t.data[:i])
		t.data = td
	}
}

// SortConfig contains the sorting information. Contains the binding order
// to use while sorting as well as the direction for each of them to use.
type SortConfig []struct {
	Binding string
	Desc    bool
}

func (s SortConfig) String() string {
	b := bytes.NewBufferString("[ ")
	for _, sc := range s {
		b.WriteString(sc.Binding)
		b.WriteString("->")
		if sc.Desc {
			b.WriteString("DESC")
		} else {
			b.WriteString("ASC")
		}
	}
	b.WriteString(" ]")
	return b.String()
}

type bySortConfig struct {
	rows []Row
	cfg  SortConfig
}

// Len returns the length of the table.
func (c bySortConfig) Len() int {
	return len(c.rows)
}

// Swap exchange the i and j rows in the table.
func (c bySortConfig) Swap(i, j int) {
	c.rows[i], c.rows[j] = c.rows[j], c.rows[i]
}

func stringLess(rsi, rsj string, desc bool) int {
	si, sj := strings.TrimSpace(rsi), strings.TrimSpace(rsj)
	if (si == "" && sj == "") || si == sj {
		return 0
	}
	b := 1
	if si < sj {
		b = -1
	}
	if desc {
		b *= -1
	}
	return b
}

// CellString create a pointer for the provided string.
func CellString(s string) *string {
	return &s
}
func rowLess(ri, rj Row, c SortConfig) bool {
	if c == nil {
		return false
	}
	cfg, last := c[0], len(c) == 1
	ci, ok := ri[cfg.Binding]
	if !ok {
		log.Fatalf("Could not retrieve binding %q! %v %v", cfg.Binding, ri, rj)
	}
	cj, ok := rj[cfg.Binding]
	if !ok {
		log.Fatalf("Could not retrieve binding %q! %v %v", cfg.Binding, ri, rj)
	}
	si, sj := "", ""
	// Check if it has a string.
	if ci.S != nil && cj.S != nil {
		si, sj = *ci.S, *cj.S
	}
	// Check if it has a nodes.
	if ci.N != nil && cj.N != nil {
		si, sj = ci.N.String(), cj.N.String()
	}
	// Check if it has a predicates.
	if ci.P != nil && cj.P != nil {
		si, sj = ci.P.String(), cj.P.String()
	}
	// Check if it has a literal.
	if ci.L != nil && cj.L != nil {
		si, sj = ci.L.ToComparableString(), cj.L.ToComparableString()
	}
	// Check if it has a time anchor.
	if ci.T != nil && cj.T != nil {
		si, sj = ci.T.Format(time.RFC3339Nano), cj.T.Format(time.RFC3339Nano)
	}
	l := stringLess(si, sj, cfg.Desc)
	if l < 0 {
		return true
	}
	if l > 0 || last {
		return false
	}
	return rowLess(ri, rj, c[1:])
}

// Less returns true if the i row is less than j one.
func (c bySortConfig) Less(i, j int) bool {
	ri, rj, cfg := c.rows[i], c.rows[j], c.cfg
	return rowLess(ri, rj, cfg)
}

// Sort sorts the table given a sort configuration.
func (t *Table) Sort(cfg SortConfig) {
	if cfg == nil {
		return
	}
	sort.Sort(bySortConfig{t.data, cfg})
}

// Accumulator type represents a generic accumulator for independent values
// expressed as the element of the array slice. Returns the values after being
// accumulated. If the wrong type is passed in, it will crash casting the
// interface.
type Accumulator interface {
	// Accumulate takes the given value and accumulates it to the current state.
	Accumulate(interface{}) (interface{}, error)

	// Resets the current state back to the original one.
	Reset()
}

// sumInt64 implements an accumulator that sum int64 values.
type sumInt64 struct {
	initialState int64
	state        int64
}

// Accumulate takes the given value and accumulates it to the current state.
func (s *sumInt64) Accumulate(v interface{}) (interface{}, error) {
	l := v.(*literal.Literal)
	iv, err := l.Int64()
	if err != nil {
		return s.state, err
	}
	s.state += iv
	return s.state, nil
}

// Resets the current state back to the original one.
func (s *sumInt64) Reset() {
	s.state = s.initialState
}

// NewSumInt64LiteralAccumulator accumulates the int64 types of a literal.
func NewSumInt64LiteralAccumulator(s int64) Accumulator {
	return &sumInt64{s, s}
}

// sumFloat64 implements an accumulator that sum float64 values.
type sumFloat64 struct {
	initialState float64
	state        float64
}

// Accumulate takes the given value and accumulates it to the current state.
func (s *sumFloat64) Accumulate(v interface{}) (interface{}, error) {
	l := v.(*literal.Literal)
	iv, err := l.Float64()
	if err != nil {
		return s.state, err
	}
	s.state += iv
	return s.state, nil
}

// Resets the current state back to the original one.
func (s *sumFloat64) Reset() {
	s.state = s.initialState
}

// NewSumFloat64LiteralAccumulator accumulates the int64 types of a literal.
func NewSumFloat64LiteralAccumulator(s float64) Accumulator {
	return &sumFloat64{s, s}
}

// countAcc implements an accumulator that count accumulation occurrences.
type countAcc struct {
	state int64
}

// Accumulate takes the given value and accumulates it to the current state.
func (c *countAcc) Accumulate(v interface{}) (interface{}, error) {
	c.state++
	return c.state, nil
}

// Resets the current state back to the original one.
func (c *countAcc) Reset() {
	c.state = 0
}

// NewCountAccumulator accumulates the int64 types of a literal.
func NewCountAccumulator() Accumulator {
	return &countAcc{0}
}

// countDistinctAcc implements an accumulator that count accumulation occurrences.
type countDistinctAcc struct {
	state map[string]int64
}

// Accumulate takes the given value and accumulates it to the current state.
func (c *countDistinctAcc) Accumulate(v interface{}) (interface{}, error) {
	vs := fmt.Sprintf("%v", v)
	c.state[vs]++
	return int64(len(c.state)), nil
}

// Resets the current state back to the original one.
func (c *countDistinctAcc) Reset() {
	c.state = make(map[string]int64)
}

// NewCountDistinctAccumulator counts calls by incrementing the internal state
// only if the value has not been seen before.
func NewCountDistinctAccumulator() Accumulator {
	return &countDistinctAcc{make(map[string]int64)}
}

// groupRangeReduce takes a sorted range and generates a new row containing
// the aggregated columns and the non aggregated ones.
func (t *Table) groupRangeReduce(i, j int, alias map[string]string, acc map[string]Accumulator) (Row, error) {
	if i > j {
		return nil, fmt.Errorf("cannot aggregate empty ranges [%d, %d)", i, j)
	}
	// Initialize the range and accumulator results.
	rng := t.data[i:j]
	vaccs := make(map[string]interface{})
	// Reset the accumulators.
	for _, a := range acc {
		a.Reset()
	}
	// Aggregate the range using the provided aggregators.
	for _, r := range rng {
		for b, a := range acc {
			av, err := a.Accumulate(r[b])
			if err != nil {
				return nil, err
			}
			vaccs[b] = av
		}
	}
	// Create a new row based on the resulting aggregations with the proper
	// binding aliasing and the non aggregated values.
	newRow := Row{}
	for b, v := range rng[0] {
		acc, ok := vaccs[b]
		if !ok {
			if a, ok := alias[b]; ok {
				newRow[a] = v
			} else {
				newRow[b] = v
			}
		} else {
			a, ok := alias[b]
			if !ok {
				return nil, fmt.Errorf("aggregated bindings require and alias; binding %s missing alias", b)
			}
			// Accumulators currently only can return numeric literals.
			switch acc.(type) {
			case int64:
				l, err := literal.DefaultBuilder().Build(literal.Int64, acc)
				if err != nil {
					return nil, err
				}
				newRow[a] = &Cell{L: l}
			case float64:
				l, err := literal.DefaultBuilder().Build(literal.Float64, acc)
				if err != nil {
					return nil, err
				}
				newRow[a] = &Cell{L: l}
			default:
				return nil, fmt.Errorf("aggregation of binding %s returned unknown value %v or type", b, acc)
			}
		}
	}
	return newRow, nil
}

// AliasAccPair contains the in, out alias, and the optional accumulator to use.
type AliasAccPair struct {
	InAlias  string
	OutAlias string
	Acc      Accumulator
}

// fullGroupRangeReduce takes a sorted range and generates a new row containing
// the aggregated columns and the non aggregated ones.
func (t *Table) fullGroupRangeReduce(i, j int, acc map[string]map[string]AliasAccPair) (Row, error) {
	if i > j {
		return nil, fmt.Errorf("cannot aggregate empty ranges [%d, %d)", i, j)
	}
	// Initialize the range and accumulator results.
	rng := t.data[i:j]
	// Reset the accumulators.
	for _, aap := range acc {
		for _, a := range aap {
			if a.Acc != nil {
				a.Acc.Reset()
			}
		}
	}
	// Aggregate the range using the provided aggregators.
	vaccs := make(map[string]map[string]interface{})
	for _, r := range rng {
		for _, aap := range acc {
			for _, a := range aap {
				if a.Acc == nil {
					continue
				}
				av, err := a.Acc.Accumulate(r[a.InAlias])
				if err != nil {
					return nil, err
				}
				if _, ok := vaccs[a.InAlias]; !ok {
					vaccs[a.InAlias] = make(map[string]interface{})
				}
				vaccs[a.InAlias][a.OutAlias] = av
			}
		}
	}
	// Create a new row based on the resulting aggregations with the proper
	// binding aliasing and the non aggregated values.
	newRow := Row{}
	for b, v := range rng[0] {
		for _, app := range acc[b] { //macc {
			if app.Acc == nil {
				newRow[app.OutAlias] = v
			} else {
				// Accumulators currently only can return numeric literals.
				switch vaccs[app.InAlias][app.OutAlias].(type) {
				case int64:
					l, err := literal.DefaultBuilder().Build(literal.Int64, vaccs[app.InAlias][app.OutAlias])
					if err != nil {
						return nil, err
					}
					newRow[app.OutAlias] = &Cell{L: l}
				case float64:
					l, err := literal.DefaultBuilder().Build(literal.Float64, vaccs[app.InAlias][app.OutAlias])
					if err != nil {
						return nil, err
					}
					newRow[app.OutAlias] = &Cell{L: l}
				default:
					return nil, fmt.Errorf("aggregation of binding %s returned unknown value %v or type", b, acc)
				}
			}
		}
	}
	if len(newRow) == 0 {
		return nil, errors.New("failed to reduced row range returning an empty one")
	}
	return newRow, nil
}

// toMap converts a list of alias and acc pairs into a nested map. The first
// key is the input binding, the second one is the output binding.
func toMap(aaps []AliasAccPair) map[string]map[string]AliasAccPair {
	resMap := make(map[string]map[string]AliasAccPair)
	for _, aap := range aaps {
		m, ok := resMap[aap.InAlias]
		if !ok {
			m = make(map[string]AliasAccPair)
			resMap[aap.InAlias] = m
		}
		m[aap.OutAlias] = aap
	}
	return resMap
}

// Reduce alters the table by sorting and then range grouping the table data.
// In order to group reduce the table, we sort the table and then apply the
// accumulator functions to each group. Finally, the table metadata gets
// updated to reflect the reduce operation.
func (t *Table) Reduce(cfg SortConfig, aaps []AliasAccPair) error {
	maaps := toMap(aaps)
	// Input validation tests.
	if len(t.bs) != len(maaps) {
		return fmt.Errorf("table.Reduce cannot project bindings; current %v, requested %v", t.bs, aaps)
	}
	for _, b := range t.bs {
		if _, ok := maaps[b]; !ok {
			return fmt.Errorf("table.Reduce missing binding alias for %q", b)
		}
	}
	cnt := 0
	for b := range maaps {
		if _, ok := t.mbs[b]; !ok {
			return fmt.Errorf("table.Reduce unknown reducer binding %q; available bindings %v", b, t.bs)
		}
		cnt++
	}
	if cnt != len(t.bs) {
		return fmt.Errorf("table.Reduce invalid reduce configuration in cfg=%v, aap=%v for table with binding %v", cfg, aaps, t.bs)
	}
	// Valid reduce configuration. Reduce sorts the table and then reduces
	// contiguous groups row groups.
	if t.NumRows() == 0 {
		return nil
	}
	t.Sort(cfg)
	last, lastIdx, current, newData := "", 0, "", []Row{}
	id := func(r Row) string {
		res := ""
		for _, c := range cfg {
			res += r[c.Binding].String()
		}
		return res
	}
	for idx, r := range t.data {
		current = id(r)
		// First time.
		if last == "" {
			last, lastIdx = current, idx
			continue
		}
		// Still in the same group.
		if last == current {
			continue
		}
		// A group reduce operation is needed.
		nr, err := t.fullGroupRangeReduce(lastIdx, idx, maaps)
		if err != nil {
			return err
		}
		newData = append(newData, nr)
		last, lastIdx = current, idx
	}
	nr, err := t.fullGroupRangeReduce(lastIdx, len(t.data), maaps)
	if err != nil {
		return err
	}
	newData = append(newData, nr)
	// Update the table.
	t.bs, t.mbs = []string{}, make(map[string]bool)
	for _, aap := range aaps {
		if !t.mbs[aap.OutAlias] {
			t.bs = append(t.bs, aap.OutAlias)
		}
		t.mbs[aap.OutAlias] = true
	}
	t.data = newData
	return nil
}

// Filter removes all the rows where the provided function returns true.
func (t *Table) Filter(f func(Row) bool) {
	var newData []Row
	for _, r := range t.data {
		if !f(r) {
			newData = append(newData, r)
		}
	}
	t.data = newData
}
