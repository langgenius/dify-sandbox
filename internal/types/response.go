package types

type DifySandboxResponse struct {
	// Code is the code of the response
	Code int `json:"code"`
	// Message is the message of the response
	Message string `json:"message"`
	// Data is the data of the response
	Data interface{} `json:"data"`
}

func SuccessResponse(data interface{}) *DifySandboxResponse {
	return &DifySandboxResponse{
		Code:    0,
		Message: "success",
		Data:    data,
	}
}

func ErrorResponse(code int, message string) *DifySandboxResponse {
	if code >= 0 {
		code = -1
	}
	return &DifySandboxResponse{
		Code:    code,
		Message: message,
	}
}
