package core

type ErrorNotFound struct {
}

func (e ErrorNotFound) Error() string {
	return "Not Found"
}

func NewErrorNotFound() ErrorNotFound {
	return ErrorNotFound{}
}

type ErrorAlreadyExists struct {
}

func (e ErrorAlreadyExists) Error() string {
	return "Already Exists"
}

func NewErrorAlreadyExists() ErrorAlreadyExists {
	return ErrorAlreadyExists{}
}

type ErrorPermissionDenied struct {
}

func (e ErrorPermissionDenied) Error() string {
	return "Permission Denied"
}

func NewErrorPermissionDenied() ErrorPermissionDenied {
	return ErrorPermissionDenied{}
}

type ErrorAlreadyDeleted struct {
}

func (e ErrorAlreadyDeleted) Error() string {
	return "Already Deleted"
}

func NewErrorAlreadyDeleted() ErrorAlreadyDeleted {
	return ErrorAlreadyDeleted{}
}
