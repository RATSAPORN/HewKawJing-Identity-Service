package errors

import "net/http"

var (
	SuccessResponse = CommonResponse{StatusCode: http.StatusOK, Message: "ok"}

	ErrorBadRequest         = CommonResponse{StatusCode: http.StatusBadRequest, Message: "bad request"}
	ErrorNotFound           = CommonResponse{StatusCode: http.StatusNotFound, Message: "resource not found"}
	ErrorInternal           = CommonResponse{StatusCode: http.StatusInternalServerError, Message: "internal server error"}
	ErrorUnauthorized       = CommonResponse{StatusCode: http.StatusUnauthorized, Message: "unauthorized"}
	ErrorForbidden          = CommonResponse{StatusCode: http.StatusForbidden, Message: "forbidden"}
	ErrorEmailExists        = CommonResponse{StatusCode: http.StatusConflict, Message: "email already registered"}
	ErrorInvalidCredentials = CommonResponse{StatusCode: http.StatusUnauthorized, Message: "invalid email or password"}
)
