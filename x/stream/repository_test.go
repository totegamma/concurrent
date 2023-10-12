package stream

import (
	"log"
	"testing"
	"context"
	"github.com/totegamma/concurrent/internal/testutil"
	"github.com/totegamma/concurrent/x/core"
)


var ctx = context.Background()
var repo Repository

func TestMain(m *testing.M) {
	log.Println("Test Start")
	db_resource, db_pool := testutil.CreateDBContainer()
	defer testutil.CloseContainer(db_resource, db_pool)

	db := testutil.ConnectDB(db_resource, db_pool)

	testutil.SetupDB(db)

	mc_resource, mc_pool := testutil.CreateMemcachedContainer()
	defer testutil.CloseContainer(mc_resource, mc_pool)

	mc := testutil.ConnectMemcached(mc_resource, mc_pool)

	repo = NewRepository(db, mc)

	m.Run()

	log.Println("Test End")
}

func TestCreateStream(t *testing.T) {
	stream := core.Stream{
		ID: "01234567890123456789",
		Visible: true,
		Author: "CC62b953CCCE898b955f256976d61BdEE04353C042",
		Maintainer: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Writer: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Reader: []string{"CC62b953CCCE898b955f256976d61BdEE04353C042"},
		Schema: "https://example.com/testschema.json",
		Payload: "{}",
	}

	created, err := repo.CreateStream(ctx, stream)
	if err != nil {
		t.Errorf("CreateStream failed: %s", err)
	}

	if created.ID != stream.ID {
		t.Errorf("CreateStream failed: ID is not matched")
	}

	if created.Visible != stream.Visible {
		t.Errorf("CreateStream failed: Visible is not matched")
	}

	if created.Author != stream.Author {
		t.Errorf("CreateStream failed: Author is not matched")
	}

	if created.Maintainer[0] != stream.Maintainer[0] {
		t.Errorf("CreateStream failed: Maintainer is not matched")
	}

	if created.Writer[0] != stream.Writer[0] {
		t.Errorf("CreateStream failed: Writer is not matched")
	}

	if created.Reader[0] != stream.Reader[0] {
		t.Errorf("CreateStream failed: Reader is not matched")
	}

	if created.Schema != stream.Schema {
		t.Errorf("CreateStream failed: Schema is not matched")
	}

	if created.Payload != stream.Payload {
		t.Errorf("CreateStream failed: Payload is not matched")
	}

	if created.CDate.IsZero() {
		t.Errorf("CreateStream failed: CreatedAt is not set")
	}

	if created.MDate.IsZero() {
		t.Errorf("CreateStream failed: UpdatedAt is not set")
	}
}
