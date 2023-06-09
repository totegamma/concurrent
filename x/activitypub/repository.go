package activitypub

import (
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
func (r Repository) GetEntityByID(id string) (ApEntity, error) {
    var entity ApEntity
    result := r.db.Where("id = ?", id).First(&entity)
    return entity, result.Error
}

// GetEntityByCCAddr returns an entity by CCAddr.
func (r Repository) GetEntityByCCAddr(ccaddr string) (ApEntity, error) {
    var entity ApEntity
    result := r.db.Where("cc_addr = ?", ccaddr).First(&entity)
    return entity, result.Error
}

// CreateEntity creates an entity.
func (r Repository) CreateEntity(entity ApEntity) (ApEntity, error) {
    result := r.db.Create(&entity)
    return entity, result.Error
}

// GetPersonByID returns a person by ID.
func (r Repository) GetPersonByID(id string) (ApPerson, error) {
    var person ApPerson
    result := r.db.Where("id = ?", id).First(&person)
    return person, result.Error
}

// UpsertPerson upserts a person.
func (r Repository) UpsertPerson(person ApPerson) (ApPerson, error) {
    result := r.db.Save(&person)
    return person, result.Error
}

// Save Follow action
func (r *Repository) SaveFollow(follow ApFollow) error {
    return r.db.Create(&follow).Error
}

// Remove Follow action
func (r *Repository) RemoveFollow(followID string) (ApFollow, error) {
    var follow ApFollow
    if err := r.db.First(&follow, "id = ?", followID).Error; err != nil {
        return ApFollow{}, err
    }
    err := r.db.Where("id = ?", followID).Delete(&ApFollow{}).Error
    if err != nil {
        return ApFollow{}, err
    }
    return follow, nil
}

