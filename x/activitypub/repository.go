package activitypub

import (
	"context"
	"gorm.io/gorm"
)

// Repository is a repository for ActivityPub.
type Repository struct {
	db *gorm.DB
}

// NewRepository returns a new Repository.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// GetEntityByID returns an entity by ID.
func (r Repository) GetEntityByID(ctx context.Context, id string) (ApEntity, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetEntityByID")
	defer span.End()

	var entity ApEntity
	result := r.db.WithContext(ctx).Where("id = ?", id).First(&entity)
	return entity, result.Error
}

// GetEntityByCCID returns an entity by CCiD.
func (r Repository) GetEntityByCCID(ctx context.Context, ccid string) (ApEntity, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetEntityByCCID")
	defer span.End()

	var entity ApEntity
	result := r.db.WithContext(ctx).Where("cc_id = ?", ccid).First(&entity)
	return entity, result.Error
}

// CreateEntity creates an entity.
func (r Repository) CreateEntity(ctx context.Context, entity ApEntity) (ApEntity, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreateEntity")
	defer span.End()

	result := r.db.WithContext(ctx).Create(&entity)
	return entity, result.Error
}

// GetPersonByID returns a person by ID.
func (r Repository) GetPersonByID(ctx context.Context, id string) (ApPerson, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetPersonByID")
	defer span.End()

	var person ApPerson
	result := r.db.WithContext(ctx).Where("id = ?", id).First(&person)
	return person, result.Error
}

// UpsertPerson upserts a person.
func (r Repository) UpsertPerson(ctx context.Context, person ApPerson) (ApPerson, error) {
	ctx, span := tracer.Start(ctx, "RepositoryUpsertPerson")
	defer span.End()

	result := r.db.WithContext(ctx).Save(&person)
	return person, result.Error
}

// Save Follow action
func (r *Repository) SaveFollow(ctx context.Context, follow ApFollow) error {
	ctx, span := tracer.Start(ctx, "RepositorySaveFollow")
	defer span.End()

	return r.db.WithContext(ctx).Create(&follow).Error
}

// GetFollowByID returns follow by ID
func (r *Repository) GetFollowByID(ctx context.Context, id string) (ApFollow, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetFollowByID")
	defer span.End()

	var follow ApFollow
	result := r.db.WithContext(ctx).Where("id = ?", id).First(&follow)
	return follow, result.Error
}

// GetAllFollows returns all Follow actions
func (r *Repository) GetAllFollows(ctx context.Context) ([]ApFollow, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetAllFollows")
	defer span.End()

	var follows []ApFollow
	err := r.db.WithContext(ctx).Find(&follows).Error
	return follows, err
}

// Remove Follow action
func (r *Repository) RemoveFollow(ctx context.Context, followID string) (ApFollow, error) {
	ctx, span := tracer.Start(ctx, "RepositoryRemoveFollow")
	defer span.End()

	var follow ApFollow
	if err := r.db.WithContext(ctx).First(&follow, "id = ?", followID).Error; err != nil {
		return ApFollow{}, err
	}
	err := r.db.WithContext(ctx).Where("id = ?", followID).Delete(&ApFollow{}).Error
	if err != nil {
		return ApFollow{}, err
	}
	return follow, nil
}
