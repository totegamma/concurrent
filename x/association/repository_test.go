package association

import (
	"context"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/totegamma/concurrent/internal/testutil"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
)

var ctx = context.Background()
var repo Repository
var db *gorm.DB

func TestMain(m *testing.M) {
	log.Println("Test Start")

	var cleanup_db func()
	_db, cleanup_db := testutil.CreateDB()
	defer cleanup_db()
	db = _db

	repo = NewRepository(_db)

	m.Run()

	log.Println("Test End")
}

func TestRepository(t *testing.T) {

	// create dummy message
	message := core.Message{
		Author:    "CC26a9C14888b558862252E185257331D2e48b3e6b",
		Schema:    "https://gammalab.net/test-message-schema.json",
		Payload:   "{}",
		Signature: "DUMMY",
	}

	err := db.WithContext(ctx).Create(&message).Error
	assert.NoError(t, err)

	// create association
	like := core.Association{
		Author:      "CCb72AAc9dcF088F7088b6718BE5a494fBB3861439",
		Schema:      "https://gammalab.net/test-like-schema.json",
		TargetID:    message.ID,
		TargetType:  "message",
		ContentHash: "like",
		Payload:     "{}",
		Variant:     "",
		Signature:   "DUMMY",
	}
	_, err = repo.Create(ctx, like)
	assert.NoError(t, err)

	emoji1 := core.Association{
		Author:      "CCb72AAc9dcF088F7088b6718BE5a494fBB3861439",
		Schema:      "https://gammalab.net/test-emoji-schema.json",
		TargetID:    message.ID,
		TargetType:  "message",
		ContentHash: "emoji1",
		Payload:     "{}",
		Variant:     "smile",
		Signature:   "DUMMY",
	}
	_, err = repo.Create(ctx, emoji1)
	assert.NoError(t, err)

	emoji2 := core.Association{
		Author:      "CCb72AAc9dcF088F7088b6718BE5a494fBB3861439",
		Schema:      "https://gammalab.net/test-emoji-schema.json",
		TargetID:    message.ID,
		TargetType:  "message",
		ContentHash: "emoji2",
		Payload:     "{}",
		Variant:     "ultrafastpolar",
		Signature:   "DUMMY",
	}
	_, err = repo.Create(ctx, emoji2)
	assert.NoError(t, err)

	emoji3 := core.Association{
		Author:      "CCb72AAc9dcF088F7088b6718BE5a494fBB3861439",
		Schema:      "https://gammalab.net/test-emoji-schema.json",
		TargetID:    message.ID,
		TargetType:  "message",
		ContentHash: "emoji3",
		Payload:     "{}",
		Variant:     "ultrafastpolar",
		Signature:   "DUMMY",
	}
	_, err = repo.Create(ctx, emoji3)
	assert.NoError(t, err)

	// test GetCountsBySchema
	results, err := repo.GetCountsBySchema(ctx, message.ID)
	if assert.NoError(t, err) {
		assert.Equal(t, 2, len(results))
	}

	// test GetBySchema
	associations, err := repo.GetBySchema(ctx, message.ID, "https://gammalab.net/test-like-schema.json")
	if assert.NoError(t, err) {
		assert.Equal(t, 1, len(associations))
	}
	associations, err = repo.GetBySchema(ctx, message.ID, "https://gammalab.net/test-emoji-schema.json")
	if assert.NoError(t, err) {
		assert.Equal(t, 3, len(associations))
	}

	// test GetCountsBySchemaAndVariant
	results, err = repo.GetCountsBySchemaAndVariant(ctx, message.ID, "https://gammalab.net/test-emoji-schema.json")
	if assert.NoError(t, err) {
		assert.Equal(t, 2, len(results))
	}

	// test GetBySchemaAndVariant
	associations, err = repo.GetBySchemaAndVariant(ctx, message.ID, "https://gammalab.net/test-emoji-schema.json", "ultrafastpolar")
	if assert.NoError(t, err) {
		assert.Equal(t, 2, len(associations))
	}

}
