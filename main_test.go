package pg_test

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	. "gopkg.in/check.v1"

	"gopkg.in/pg.v3"
)

func TestUnixSocket(t *testing.T) {
	db := pg.Connect(&pg.Options{
		Network:  "unix",
		Host:     "/var/run/postgresql/.s.PGSQL.5432",
		User:     "postgres",
		Database: "test",
	})
	_, err := db.Exec("SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
}

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&DBTest{})

type DBTest struct {
	db *pg.DB
}

func (t *DBTest) SetUpTest(c *C) {
	t.db = pgdb()
}

func (t *DBTest) TearDownTest(c *C) {
	c.Assert(t.db.Close(), IsNil)
}

type discard struct{}

func (l *discard) New() interface{} {
	return l
}

func (l *discard) Load(colIdx int, colName string, b []byte) error {
	return nil
}

func (t *DBTest) TestQueryZeroRows(c *C) {
	res, err := t.db.Query(&discard{}, "SELECT 1 WHERE 1 != 1")
	c.Assert(err, IsNil)
	c.Assert(res.Affected(), Equals, 0)
}

func (t *DBTest) TestQueryOneErrNoRows(c *C) {
	_, err := t.db.QueryOne(&discard{}, "SELECT 1 WHERE 1 != 1")
	c.Assert(err, Equals, pg.ErrNoRows)
}

func (t *DBTest) TestQueryOneErrMultiRows(c *C) {
	_, err := t.db.QueryOne(&discard{}, "SELECT generate_series(0, 1)")
	c.Assert(err, Equals, pg.ErrMultiRows)
}

func (t *DBTest) TestExecOne(c *C) {
	res, err := t.db.ExecOne("SELECT 1")
	c.Assert(err, IsNil)
	c.Assert(res.Affected(), Equals, 1)
}

func (t *DBTest) TestExecOneErrNoRows(c *C) {
	_, err := t.db.ExecOne("SELECT 1 WHERE 1 != 1")
	c.Assert(err, Equals, pg.ErrNoRows)
}

func (t *DBTest) TestExecOneErrMultiRows(c *C) {
	_, err := t.db.ExecOne("SELECT generate_series(0, 1)")
	c.Assert(err, Equals, pg.ErrMultiRows)
}

func (t *DBTest) TestLoadInto(c *C) {
	var dst int
	_, err := t.db.QueryOne(pg.LoadInto(&dst), "SELECT 1")
	c.Assert(err, IsNil)
	c.Assert(dst, Equals, 1)
}

func (t *DBTest) TestExec(c *C) {
	res, err := t.db.Exec("CREATE TEMP TABLE test(id serial PRIMARY KEY)")
	c.Assert(err, IsNil)
	c.Assert(res.Affected(), Equals, 0)

	res, err = t.db.Exec("INSERT INTO test VALUES (1)")
	c.Assert(err, IsNil)
	c.Assert(res.Affected(), Equals, 1)
}

func (t *DBTest) TestStatementExec(c *C) {
	res, err := t.db.Exec("CREATE TEMP TABLE test(id serial PRIMARY KEY)")
	c.Assert(err, IsNil)
	c.Assert(res.Affected(), Equals, 0)

	stmt, err := t.db.Prepare("INSERT INTO test VALUES($1)")
	c.Assert(err, IsNil)
	defer stmt.Close()

	res, err = stmt.Exec(1)
	c.Assert(err, IsNil)
	c.Assert(res.Affected(), Equals, 1)
}

func (t *DBTest) TestLargeWriteRead(c *C) {
	src := bytes.Repeat([]byte{0x1}, 1e6)
	var dst []byte
	_, err := t.db.QueryOne(pg.LoadInto(&dst), "SELECT ?", src)
	c.Assert(err, IsNil)
	c.Assert(dst, DeepEquals, src)
}

func (t *DBTest) TestIntegrityError(c *C) {
	_, err := t.db.Exec("DO $$BEGIN RAISE unique_violation USING MESSAGE='foo'; END$$;")
	c.Assert(err, FitsTypeOf, &pg.IntegrityError{})
}

func deref(viface interface{}) interface{} {
	v := reflect.ValueOf(viface)
	for v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.IsValid() {
		return v.Interface()
	}
	return nil
}

func zero(v interface{}) interface{} {
	return reflect.Zero(reflect.ValueOf(v).Elem().Type()).Interface()
}

type customStrSlice []string

func (s customStrSlice) Value() (driver.Value, error) {
	return strings.Join(s, "\n"), nil
}

func (s *customStrSlice) Scan(v interface{}) error {
	if v == nil {
		*s = nil
		return nil
	}

	b := v.([]byte)

	if len(b) == 0 {
		*s = []string{}
		return nil
	}

	*s = strings.Split(string(b), "\n")
	return nil
}

var (
	boolv   bool
	boolptr *bool

	stringv   string
	stringptr *string
	bytesv    []byte

	intv     int
	intvptr  *int
	int8v    int8
	int16v   int16
	int32v   int32
	int64v   int64
	uintv    uint
	uint8v   uint8
	uint16v  uint16
	uint32v  uint32
	uint64v  uint64
	uintptrv uintptr

	f32v float32
	f64v float64

	strslice []string
	intslice []int

	strstrmap map[string]string

	nullBool    sql.NullBool
	nullString  sql.NullString
	nullInt64   sql.NullInt64
	nullFloat64 sql.NullFloat64

	customStrSliceV customStrSlice

	timev   time.Time
	timeptr *time.Time

	pgints    pg.Ints
	pgstrings pg.Strings
)

type jsonStruct struct {
	Foo string
}

type jsonMap_ map[string]interface{}

func (m *jsonMap_) Scan(value interface{}) error {
	return json.Unmarshal(value.([]byte), m)
}

func (m jsonMap_) Value() (driver.Value, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

type conversionTest struct {
	src, dst, wanted interface{}
	pgtype           string

	wanterr  string
	wantnil  bool
	wantzero bool
}

var conversionTests = []conversionTest{
	{src: true, dst: nil, wanterr: "pg: Decode(nil)"},
	{src: true, dst: &uintptrv, wanterr: "pg: unsupported dst: uintptr"},
	{src: true, dst: boolv, wanterr: "pg: Decode(nonsettable bool)"},
	{src: true, dst: boolptr, wanterr: "pg: Decode(nonsettable *bool)"},

	{src: false, dst: &boolv, pgtype: "bool"},
	{src: true, dst: &boolv, pgtype: "bool"},
	{src: nil, dst: &boolv, pgtype: "bool", wantzero: true},
	{src: true, dst: &boolptr, pgtype: "bool"},
	{src: nil, dst: &boolptr, pgtype: "bool", wantnil: true},

	{src: "hello world", dst: &stringv, pgtype: "text"},
	{src: nil, dst: &stringv, pgtype: "text", wantzero: true},
	{src: "hello world", dst: &stringptr, pgtype: "text"},
	{src: nil, dst: &stringptr, pgtype: "text", wantnil: true},

	{src: []byte("hello world\000"), dst: &bytesv, pgtype: "bytea"},
	{src: []byte{}, dst: &bytesv, pgtype: "bytea", wantzero: true},
	{src: nil, dst: &bytesv, pgtype: "bytea", wantnil: true},

	{src: int(math.MaxInt32), dst: &intv, pgtype: "int"},
	{src: int(math.MinInt32), dst: &intv, pgtype: "int"},
	{src: nil, dst: &intv, pgtype: "int", wantzero: true},
	{src: int(math.MaxInt32), dst: &intvptr, pgtype: "int"},
	{src: nil, dst: &intvptr, pgtype: "int", wantnil: true},
	{src: int8(math.MaxInt8), dst: &int8v, pgtype: "smallint"},
	{src: int8(math.MinInt8), dst: &int8v, pgtype: "smallint"},
	{src: int16(math.MaxInt16), dst: &int16v, pgtype: "smallint"},
	{src: int16(math.MinInt16), dst: &int16v, pgtype: "smallint"},
	{src: int32(math.MaxInt32), dst: &int32v, pgtype: "int"},
	{src: int32(math.MinInt32), dst: &int32v, pgtype: "int"},
	{src: int64(math.MaxInt64), dst: &int64v, pgtype: "bigint"},
	{src: int64(math.MinInt64), dst: &int64v, pgtype: "bigint"},
	{src: uint(math.MaxUint32), dst: &uintv, pgtype: "bigint"},
	{src: uint8(math.MaxUint8), dst: &uint8v, pgtype: "smallint"},
	{src: uint16(math.MaxUint16), dst: &uint16v, pgtype: "int"},
	{src: uint32(math.MaxUint32), dst: &uint32v, pgtype: "bigint"},
	{src: uint64(math.MaxUint64), dst: &uint64v},

	{src: float32(math.MaxFloat32), dst: &f32v, pgtype: "decimal"},
	{src: float32(math.SmallestNonzeroFloat32), dst: &f32v, pgtype: "decimal"},
	{src: float64(math.MaxFloat64), dst: &f64v, pgtype: "decimal"},
	{src: float64(math.SmallestNonzeroFloat64), dst: &f64v, pgtype: "decimal"},

	{src: []string{"foo\n", "bar {}", "'\\\""}, dst: &strslice, pgtype: "text[]"},
	{src: []string{}, dst: &strslice, pgtype: "text[]", wantzero: true},
	{src: nil, dst: &strslice, pgtype: "text[]", wantnil: true},

	{src: []int{}, dst: &intslice, pgtype: "int[]"},
	{src: []int{1, 2, 3}, dst: &intslice, pgtype: "int[]"},

	{
		src:    map[string]string{"foo\n =>": "bar\n =>", "'\\\"": "'\\\""},
		dst:    &strstrmap,
		pgtype: "hstore",
	},

	{src: &sql.NullBool{}, dst: &nullBool, pgtype: "bool"},
	{src: &sql.NullBool{Valid: true}, dst: &nullBool, pgtype: "bool"},
	{src: &sql.NullBool{Valid: true, Bool: true}, dst: &nullBool, pgtype: "bool"},

	{src: &sql.NullString{}, dst: &nullString, pgtype: "text"},
	{src: &sql.NullString{Valid: true}, dst: &nullString, pgtype: "text"},
	{src: &sql.NullString{Valid: true, String: "foo"}, dst: &nullString, pgtype: "text"},

	{src: &sql.NullInt64{}, dst: &nullInt64, pgtype: "bigint"},
	{src: &sql.NullInt64{Valid: true}, dst: &nullInt64, pgtype: "bigint"},
	{src: &sql.NullInt64{Valid: true, Int64: math.MaxInt64}, dst: &nullInt64, pgtype: "bigint"},

	{src: &sql.NullFloat64{}, dst: &nullFloat64, pgtype: "decimal"},
	{src: &sql.NullFloat64{Valid: true}, dst: &nullFloat64, pgtype: "decimal"},
	{src: &sql.NullFloat64{Valid: true, Float64: math.MaxFloat64}, dst: &nullFloat64, pgtype: "decimal"},

	{src: customStrSlice{}, dst: &customStrSliceV, wantzero: true},
	{src: nil, dst: &customStrSliceV, wantnil: true},
	{src: customStrSlice{"one", "two"}, dst: &customStrSliceV},

	{src: time.Time{}, dst: &timev, pgtype: "timestamp"},
	{src: time.Now(), dst: &timev, pgtype: "timestamp"},
	{src: time.Now().UTC(), dst: &timev, pgtype: "timestamp"},
	{src: nil, dst: &timev, pgtype: "timestamp", wantzero: true},
	{src: time.Now(), dst: &timeptr, pgtype: "timestamp"},
	{src: nil, dst: &timeptr, pgtype: "timestamp", wantnil: true},

	{src: time.Time{}, dst: &timev, pgtype: "timestamptz"},
	{src: time.Now(), dst: &timev, pgtype: "timestamptz"},
	{src: time.Now().UTC(), dst: &timev, pgtype: "timestamptz"},
	{src: nil, dst: &timev, pgtype: "timestamptz", wantzero: true},
	{src: time.Now(), dst: &timeptr, pgtype: "timestamptz"},
	{src: nil, dst: &timeptr, pgtype: "timestamptz", wantnil: true},

	{src: `{"foo": "bar"}`, dst: &jsonStruct{}, wanted: jsonStruct{Foo: "bar"}},
	{src: jsonMap_{"foo": "bar"}, dst: &jsonMap_{}, pgtype: "json"},

	{src: pg.Ints{1, 2, 3}, dst: &pgints},
	{src: pg.Strings{"hello", "world"}, dst: &pgstrings},
}

func (t *conversionTest) Assert(c *C, err error) {
	if t.wanterr != "" {
		c.Assert(err, Not(IsNil), t.Comment())
		c.Assert(err.Error(), Equals, t.wanterr, t.Comment())
		return
	}
	c.Assert(err, IsNil, t.Comment())

	src := deref(t.src)
	dst := deref(t.dst)

	if t.wantzero {
		if reflect.ValueOf(dst).Kind() == reflect.Slice {
			c.Assert(dst, Not(IsNil), t.Comment())
			c.Assert(dst, HasLen, 0, t.Comment())
		} else {
			c.Assert(dst, Equals, zero(t.dst), t.Comment())
		}
		return
	}

	if t.wantnil {
		c.Assert(reflect.ValueOf(t.dst).Elem().IsNil(), Equals, true, t.Comment())
		return
	}

	if dsttm, ok := dst.(time.Time); ok {
		srctm := src.(time.Time)
		c.Assert(dsttm.Unix(), Equals, srctm.Unix(), t.Comment())
		return
	}

	if t.wanted != nil {
		c.Assert(dst, DeepEquals, t.wanted, t.Comment())
	} else {
		c.Assert(dst, DeepEquals, src, t.Comment())
	}
}

func (t *conversionTest) Comment() CommentInterface {
	return Commentf("src: %#v, dst: %#v", t.src, t.dst)
}

func (t *DBTest) TestConversion(c *C) {
	t.db.Exec("CREATE EXTENSION hstore")
	defer t.db.Exec("DROP EXTENSION hstore")

	for _, test := range conversionTests {
		_, err := t.db.QueryOne(pg.LoadInto(test.dst), "SELECT (?) AS dst", test.src)
		test.Assert(c, err)
	}

	for _, test := range conversionTests {
		if test.pgtype == "" {
			continue
		}

		stmt, err := t.db.Prepare(fmt.Sprintf("SELECT ($1::%s) AS dst", test.pgtype))
		c.Assert(err, IsNil)

		_, err = stmt.QueryOne(pg.LoadInto(test.dst), test.src)
		c.Assert(stmt.Close(), IsNil, test.Comment())
		test.Assert(c, err)
	}

	for _, test := range conversionTests {
		dst := struct{ Dst interface{} }{Dst: test.dst}
		_, err := t.db.QueryOne(&dst, "SELECT (?) AS dst", test.src)
		test.Assert(c, err)
	}

	for _, test := range conversionTests {
		if test.pgtype == "" {
			continue
		}

		stmt, err := t.db.Prepare(fmt.Sprintf("SELECT ($1::%s) AS dst", test.pgtype))
		c.Assert(err, IsNil)

		dst := struct{ Dst interface{} }{Dst: test.dst}
		_, err = stmt.QueryOne(&dst, test.src)
		c.Assert(stmt.Close(), IsNil)
		test.Assert(c, err)
	}
}

func (t *DBTest) TestScannerValueOnStruct(c *C) {
	src := customStrSlice{"foo", "bar"}
	dst := struct{ Dst customStrSlice }{}
	_, err := t.db.QueryOne(&dst, "SELECT ? AS dst", src)
	c.Assert(err, IsNil)
	c.Assert(dst.Dst, DeepEquals, src)
}

var timeTests = []struct {
	str    string
	wanted time.Time
}{
	{"2001-02-03", time.Date(2001, time.February, 3, 0, 0, 0, 0, time.UTC)},
	{"2001-02-03 04:05:06", time.Date(2001, time.February, 3, 4, 5, 6, 0, time.Local)},
	{"2001-02-03 04:05:06.000001", time.Date(2001, time.February, 3, 4, 5, 6, 1000, time.Local)},
	{"2001-02-03 04:05:06.00001", time.Date(2001, time.February, 3, 4, 5, 6, 10000, time.Local)},
	{"2001-02-03 04:05:06.0001", time.Date(2001, time.February, 3, 4, 5, 6, 100000, time.Local)},
	{"2001-02-03 04:05:06.001", time.Date(2001, time.February, 3, 4, 5, 6, 1000000, time.Local)},
	{"2001-02-03 04:05:06.01", time.Date(2001, time.February, 3, 4, 5, 6, 10000000, time.Local)},
	{"2001-02-03 04:05:06.1", time.Date(2001, time.February, 3, 4, 5, 6, 100000000, time.Local)},
	{"2001-02-03 04:05:06.12", time.Date(2001, time.February, 3, 4, 5, 6, 120000000, time.Local)},
	{"2001-02-03 04:05:06.123", time.Date(2001, time.February, 3, 4, 5, 6, 123000000, time.Local)},
	{"2001-02-03 04:05:06.1234", time.Date(2001, time.February, 3, 4, 5, 6, 123400000, time.Local)},
	{"2001-02-03 04:05:06.12345", time.Date(2001, time.February, 3, 4, 5, 6, 123450000, time.Local)},
	{"2001-02-03 04:05:06.123456", time.Date(2001, time.February, 3, 4, 5, 6, 123456000, time.Local)},
	{"2001-02-03 04:05:06.123-07", time.Date(2001, time.February, 3, 4, 5, 6, 123000000, time.FixedZone("", -7*60*60))},
	{"2001-02-03 04:05:06-07", time.Date(2001, time.February, 3, 4, 5, 6, 0, time.FixedZone("", -7*60*60))},
	{"2001-02-03 04:05:06-07:42", time.Date(2001, time.February, 3, 4, 5, 6, 0, time.FixedZone("", -(7*60*60+42*60)))},
	{"2001-02-03 04:05:06-07:30:09", time.Date(2001, time.February, 3, 4, 5, 6, 0, time.FixedZone("", -(7*60*60+30*60+9)))},
	{"2001-02-03 04:05:06+07", time.Date(2001, time.February, 3, 4, 5, 6, 0, time.FixedZone("", 7*60*60))},
}

func (t *DBTest) TestTime(c *C) {
	for _, test := range timeTests {
		var tm time.Time
		_, err := t.db.QueryOne(pg.LoadInto(&tm), "SELECT ?", test.str)
		c.Assert(err, IsNil)
		c.Assert(tm.Unix(), Equals, test.wanted.Unix(), Commentf("str=%q", test.str))
	}
}

func (t *DBTest) TestCopyFrom(c *C) {
	data := "hello\t5\nworld\t5\nfoo\t3\nbar\t3\n"

	_, err := t.db.Exec("CREATE TEMP TABLE test(word text, len int)")
	c.Assert(err, IsNil)

	r := strings.NewReader(data)
	res, err := t.db.CopyFrom(r, "COPY test FROM STDIN")
	c.Assert(err, IsNil)
	c.Assert(res.Affected(), Equals, 4)

	buf := &bytes.Buffer{}
	res, err = t.db.CopyTo(&NopWriteCloser{buf}, "COPY test TO STDOUT")
	c.Assert(err, IsNil)
	c.Assert(res.Affected(), Equals, 4)
	c.Assert(buf.String(), Equals, data)
}

func (t *DBTest) TestCopyTo(c *C) {
	_, err := t.db.Exec("CREATE TEMP TABLE test(n int)")
	c.Assert(err, IsNil)

	_, err = t.db.Exec("INSERT INTO test SELECT generate_series(1, 1000000)")
	c.Assert(err, IsNil)

	buf := &bytes.Buffer{}
	res, err := t.db.CopyTo(&NopWriteCloser{buf}, "COPY test TO STDOUT")
	c.Assert(err, IsNil)
	c.Assert(res.Affected(), Equals, 1000000)

	_, err = t.db.Exec("CREATE TEMP TABLE test2(n int)")
	c.Assert(err, IsNil)

	res, err = t.db.CopyFrom(buf, "COPY test2 FROM STDIN")
	c.Assert(err, IsNil)
	c.Assert(res.Affected(), Equals, 1000000)
}
