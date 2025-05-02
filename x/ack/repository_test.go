package ack

import (
	"testing"

	"github.com/concrnt/concrnt/core"
	"github.com/concrnt/concrnt/internal/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRepository(t *testing.T) {
	ctx := t.Context()

	db, cleanup := testutil.CreateDB()
	defer cleanup()

	repo := NewRepository(db)

	// Test Ack
	t.Run("Ack", func(t *testing.T) {
		user1 := "con1nadqu3d7pht036lpf68z4ftj909p428umuwau8"
		user2 := "con1xw54ufkh8ts29qk5rxvw0t4q9p6hmpmgzh9tah"
		ack := core.Ack{
			From:      user1,
			To:        user2,
			Document:  `"doc1"`,
			Signature: "sig1",
		}
		createdAck, err := repo.Ack(ctx, &ack)
		assert.NoError(t, err)
		assert.True(t, createdAck.Valid)
		assert.Equal(t, user1, createdAck.From)
		assert.Equal(t, user2, createdAck.To)

		// Verify in DB using composite key
		var foundAck core.Ack
		err = db.WithContext(ctx).First(&foundAck, "\"from\" = ? AND \"to\" = ?", createdAck.From, createdAck.To).Error
		assert.NoError(t, err)
		assert.True(t, foundAck.Valid)
		assert.Equal(t, user1, foundAck.From)
		assert.Equal(t, user2, foundAck.To)
	})

	// Test GetAcker
	t.Run("GetAcker", func(t *testing.T) {
		user2 := "con1xw54ufkh8ts29qk5rxvw0t4q9p6hmpmgzh9tah"
		user3 := "con1uwssx3d80e5wwl9fg8nnjpqms8rvs8p2lcldlr"
		// Create another ack for the same 'to' user
		ack2 := core.Ack{
			From:      user3,
			To:        user2,
			Document:  `"doc2"`,
			Signature: "sig2",
		}
		_, err := repo.Ack(ctx, &ack2)
		assert.NoError(t, err)

		acks, err := repo.GetAcker(ctx, user2)
		assert.NoError(t, err)
		assert.Len(t, acks, 2)
		// Add more specific checks if needed
	})

	// Test GetAcking
	t.Run("GetAcking", func(t *testing.T) {
		user1 := "con1nadqu3d7pht036lpf68z4ftj909p428umuwau8"
		user2 := "con1xw54ufkh8ts29qk5rxvw0t4q9p6hmpmgzh9tah"
		user3 := "con1uwssx3d80e5wwl9fg8nnjpqms8rvs8p2lcldlr"
		acks, err := repo.GetAcking(ctx, user1)
		assert.NoError(t, err)
		assert.Len(t, acks, 1)
		assert.Equal(t, user2, acks[0].To)

		acks, err = repo.GetAcking(ctx, user3)
		assert.NoError(t, err)
		assert.Len(t, acks, 1)
		assert.Equal(t, user2, acks[0].To)
	})

	// Test Unack
	t.Run("Unack", func(t *testing.T) {
		user1 := "con1nadqu3d7pht036lpf68z4ftj909p428umuwau8"
		user2 := "con1xw54ufkh8ts29qk5rxvw0t4q9p6hmpmgzh9tah"
		user3 := "con1uwssx3d80e5wwl9fg8nnjpqms8rvs8p2lcldlr"
		// Get the first ack again to unack it using composite key
		var ackToUnack core.Ack
		err := db.WithContext(ctx).First(&ackToUnack, "\"from\" = ? AND \"to\" = ?", user1, user2).Error
		assert.NoError(t, err)

		unackedAck, err := repo.Unack(ctx, &ackToUnack)
		assert.NoError(t, err)
		assert.False(t, unackedAck.Valid)

		// Verify in DB using composite key
		var foundAck core.Ack
		err = db.WithContext(ctx).First(&foundAck, "\"from\" = ? AND \"to\" = ?", unackedAck.From, unackedAck.To).Error
		assert.NoError(t, err)
		assert.False(t, foundAck.Valid)

		// Check GetAcker again, should only find the valid one
		acks, err := repo.GetAcker(ctx, user2)
		assert.NoError(t, err)
		assert.Len(t, acks, 1)
		assert.Equal(t, user3, acks[0].From)
	})
}
