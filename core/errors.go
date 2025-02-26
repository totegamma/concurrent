package core

type ErrorTypeNotFound struct {
	Type    string
	Message string
}

var ErrorNotFound = ErrorTypeNotFound{Type: "ConcrntErrorNotFound"}

func (e ErrorTypeNotFound) Error() string {
	if e.Message != "" {
		return "Not Found: " + e.Message
	}
	return "Not Found"
}

func (e ErrorTypeNotFound) Is(err error) bool {
	f, ok := err.(ErrorTypeNotFound)
	if ok {
		return e.Type == f.Type
	}
	return false
}

func NewErrorNotFound() ErrorTypeNotFound {
	return ErrorTypeNotFound{Type: "ConcrntErrorNotFound"}
}

func NewErrorNotFoundWithMsg(msg string) ErrorTypeNotFound {
	return ErrorTypeNotFound{Type: "ConcrntErrorNotFound", Message: msg}
}

// ------

type ErrorTypeAlreadyExists struct {
	Type string
}

var ErrorAlreadyExists = ErrorTypeAlreadyExists{Type: "ConcrntErrorAlreadyExists"}

func (e ErrorTypeAlreadyExists) Is(err error) bool {
	f, ok := err.(ErrorTypeAlreadyExists)
	if ok {
		return e.Type == f.Type
	}
	return false
}

func (e ErrorTypeAlreadyExists) Error() string {
	return "Already Exists"
}

func NewErrorAlreadyExists() ErrorTypeAlreadyExists {
	return ErrorTypeAlreadyExists{Type: "ConcrntErrorAlreadyExists"}
}

// ------

type ErrorTypePermissionDenied struct {
	Type string
}

var ErrorPermissionDenied = ErrorTypePermissionDenied{Type: "ConcrntErrorPermissionDenied"}

func (e ErrorTypePermissionDenied) Is(err error) bool {
	f, ok := err.(ErrorTypePermissionDenied)
	if ok {
		return e.Type == f.Type
	}
	return false
}

func (e ErrorTypePermissionDenied) Error() string {
	return "Permission Denied"
}

func NewErrorPermissionDenied() ErrorTypePermissionDenied {
	return ErrorTypePermissionDenied{Type: "ConcrntErrorPermissionDenied"}
}

// ------

type ErrorTypeAlreadyDeleted struct {
	Type string
}

var ErrorAlreadyDeleted = ErrorTypeAlreadyDeleted{Type: "ConcrntErrorAlreadyDeleted"}

func (e ErrorTypeAlreadyDeleted) Is(err error) bool {
	f, ok := err.(ErrorTypeAlreadyDeleted)
	if ok {
		return e.Type == f.Type
	}
	return false
}

func (e ErrorTypeAlreadyDeleted) Error() string {
	return "Already Deleted"
}

func NewErrorAlreadyDeleted() ErrorTypeAlreadyDeleted {
	return ErrorTypeAlreadyDeleted{Type: "ConcrntErrorAlreadyDeleted"}
}
