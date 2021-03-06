package pg_test

import (
	. "gopkg.in/check.v1"

	"gopkg.in/pg.v3"
)

var _ = Suite(&TxTest{})

type TxTest struct {
	db *pg.DB
}

func (t *TxTest) SetUpTest(c *C) {
	t.db = pg.Connect(&pg.Options{
		User:     "postgres",
		Database: "test",
		PoolSize: 10,
	})
}

func (t *TxTest) TearDownTest(c *C) {
	c.Assert(t.db.Close(), IsNil)
}

func (t *TxTest) TestMultiPrepare(c *C) {
	tx, err := t.db.Begin()
	c.Assert(err, IsNil)

	stmt1, err := tx.Prepare(`SELECT 1`)
	c.Assert(err, IsNil)

	stmt2, err := tx.Prepare(`SELECT 2`)
	c.Assert(err, IsNil)

	var n1 int
	_, err = stmt1.QueryOne(pg.LoadInto(&n1))
	c.Assert(err, IsNil)
	c.Assert(n1, Equals, 1)

	var n2 int
	_, err = stmt2.QueryOne(pg.LoadInto(&n2))
	c.Assert(err, IsNil)
	c.Assert(n2, Equals, 2)

	c.Assert(tx.Rollback(), IsNil)
}
