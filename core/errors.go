package core

type ErrorNotFound struct {
	Message string
}

func (e ErrorNotFound) Error() string {
	if e.Message != "" {
		return "Not Found: " + e.Message
	}
	return "Not Found"
}

func NewErrorNotFound() ErrorNotFound {
	return ErrorNotFound{}
}

func NewErrorNotFoundWithMsg(msg string) ErrorNotFound {
	return ErrorNotFound{Message: msg}
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
