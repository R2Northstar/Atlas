package api0

import (
	"fmt"
)

// ErrorCode represents a known Northstar error code.
type ErrorCode string

// https://github.com/R2Northstar/NorthstarMasterServer/blob/b45ff0ef267712e8bff6cd718bb5dc1afcdec420/shared/errorcodes.js
const (
	ErrorCode_NO_GAMESERVER_RESPONSE     ErrorCode = "NO_GAMESERVER_RESPONSE"     // Couldn't reach game server
	ErrorCode_BAD_GAMESERVER_RESPONSE    ErrorCode = "BAD_GAMESERVER_RESPONSE"    // Game server gave an invalid response
	ErrorCode_UNAUTHORIZED_GAMESERVER    ErrorCode = "UNAUTHORIZED_GAMESERVER"    // Game server is not authorized to make that request
	ErrorCode_UNAUTHORIZED_GAME          ErrorCode = "UNAUTHORIZED_GAME"          // Stryder couldn't confirm that this account owns Titanfall 2
	ErrorCode_UNAUTHORIZED_PWD           ErrorCode = "UNAUTHORIZED_PWD"           // Wrong password
	ErrorCode_STRYDER_RESPONSE           ErrorCode = "STRYDER_RESPONSE"           // Got bad response from stryder
	ErrorCode_STRYDER_PARSE              ErrorCode = "STRYDER_PARSE"              // Couldn't parse stryder response
	ErrorCode_PLAYER_NOT_FOUND           ErrorCode = "PLAYER_NOT_FOUND"           // Couldn't find player account
	ErrorCode_INVALID_MASTERSERVER_TOKEN ErrorCode = "INVALID_MASTERSERVER_TOKEN" // Invalid or expired masterserver token
	ErrorCode_JSON_PARSE_ERROR           ErrorCode = "JSON_PARSE_ERROR"           // Error parsing json response
	ErrorCode_UNSUPPORTED_VERSION        ErrorCode = "UNSUPPORTED_VERSION"        // The version you are using is no longer supported
	ErrorCode_DUPLICATE_SERVER           ErrorCode = "DUPLICATE_SERVER"           // A server with this port already exists for your IP address
	ErrorCode_CONNECTION_REJECTED        ErrorCode = "CONNECTION_REJECTED"        // Connection rejected
)

const (
	ErrorCode_INTERNAL_SERVER_ERROR ErrorCode = "INTERNAL_SERVER_ERROR"
	ErrorCode_BAD_REQUEST           ErrorCode = "BAD_REQUEST"
)

// ErrorObj contains an error code and a message for API responses.
type ErrorObj struct {
	Code    ErrorCode `json:"enum"`
	Message string    `json:"msg"` // note: no omitempty
}

// Obj returns an ErrorObj.
func (n ErrorCode) Obj() ErrorObj {
	return ErrorObj{
		Code: n,
	}
}

// MessageObj is like Message, but returns an ErrorObj.
func (n ErrorCode) MessageObj() ErrorObj {
	return ErrorObj{
		Code:    n,
		Message: n.Message(),
	}
}

// MessageObjf is like Messagef, but returns an ErrorObj.
func (n ErrorCode) MessageObjf(format string, a ...interface{}) ErrorObj {
	return ErrorObj{
		Code:    n,
		Message: n.Messagef(format, a...),
	}
}

// Message returns the default message for error code n.
func (n ErrorCode) Message() string {
	switch n {
	case ErrorCode_NO_GAMESERVER_RESPONSE:
		return "Couldn't reach game server"
	case ErrorCode_BAD_GAMESERVER_RESPONSE:
		return "Game server gave an invalid response"
	case ErrorCode_UNAUTHORIZED_GAMESERVER:
		return "Game server is not authorized to make that request"
	case ErrorCode_UNAUTHORIZED_GAME:
		return "Stryder couldn't confirm that this account owns Titanfall 2"
	case ErrorCode_UNAUTHORIZED_PWD:
		return "Wrong password"
	case ErrorCode_STRYDER_RESPONSE:
		return "Got bad response from stryder"
	case ErrorCode_STRYDER_PARSE:
		return "Couldn't parse stryder response"
	case ErrorCode_PLAYER_NOT_FOUND:
		return "Couldn't find player account"
	case ErrorCode_INVALID_MASTERSERVER_TOKEN:
		return "Invalid or expired masterserver token"
	case ErrorCode_JSON_PARSE_ERROR:
		return "Error parsing json response"
	case ErrorCode_UNSUPPORTED_VERSION:
		return "The version you are using is no longer supported"
	case ErrorCode_DUPLICATE_SERVER:
		return "A server with this port already exists for your IP address"
	case ErrorCode_INTERNAL_SERVER_ERROR:
		return "Internal server error"
	case ErrorCode_BAD_REQUEST:
		return "Bad request"
	case ErrorCode_CONNECTION_REJECTED:
		return "Connection rejected"
	default:
		return string(n)
	}
}

// Messagef returns Message() with additional text appended after ": ".
func (n ErrorCode) Messagef(format string, a ...interface{}) string {
	if format == "" {
		return n.Message()
	}
	return n.Message() + ": " + fmt.Sprintf(format, a...)
}
