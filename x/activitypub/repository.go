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

