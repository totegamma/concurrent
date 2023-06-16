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
    ctx, childSpan := tracer.Start(ctx, "RepositoryGetEntityByID")
    defer childSpan.End()

    var entity ApEntity
    result := r.db.WithContext(ctx).Where("id = ?", id).First(&entity)
    return entity, result.Error
}

// GetEntityByCCAddr returns an entity by CCAddr.
func (r Repository) GetEntityByCCAddr(ctx context.Context, ccaddr string) (ApEntity, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryGetEntityByCCAddr")
    defer childSpan.End()

    var entity ApEntity
    result := r.db.WithContext(ctx).Where("cc_addr = ?", ccaddr).First(&entity)
    return entity, result.Error
}

// CreateEntity creates an entity.
func (r Repository) CreateEntity(ctx context.Context, entity ApEntity) (ApEntity, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryCreateEntity")
    defer childSpan.End()

    result := r.db.WithContext(ctx).Create(&entity)
    return entity, result.Error
}

// GetPersonByID returns a person by ID.
func (r Repository) GetPersonByID(ctx context.Context, id string) (ApPerson, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryGetPersonByID")
    defer childSpan.End()

    var person ApPerson
    result := r.db.WithContext(ctx).Where("id = ?", id).First(&person)
    return person, result.Error
}

// UpsertPerson upserts a person.
func (r Repository) UpsertPerson(ctx context.Context, person ApPerson) (ApPerson, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryUpsertPerson")
    defer childSpan.End()

    result := r.db.WithContext(ctx).Save(&person)
    return person, result.Error
}

// Save Follow action
func (r *Repository) SaveFollow(ctx context.Context, follow ApFollow) error {
    ctx, childSpan := tracer.Start(ctx, "RepositorySaveFollow")
    defer childSpan.End()

    return r.db.WithContext(ctx).Create(&follow).Error
}

// GetFollowByID returns follow by ID
func (r *Repository) GetFollowByID(ctx context.Context, id string) (ApFollow, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryGetFollowByID")
    defer childSpan.End()

    var follow ApFollow
    result := r.db.WithContext(ctx).Where("id = ?", id).First(&follow)
    return follow, result.Error
}

// GetAllFollows returns all Follow actions
func (r *Repository) GetAllFollows(ctx context.Context) ([]ApFollow, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryGetAllFollows")
    defer childSpan.End()

    var follows []ApFollow
    err := r.db.WithContext(ctx).Find(&follows).Error
    return follows, err
}

// Remove Follow action
func (r *Repository) RemoveFollow(ctx context.Context, followID string) (ApFollow, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryRemoveFollow")
    defer childSpan.End()

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

