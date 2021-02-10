// Package awswebsocketadapter can be used to run an AWS Lambda function integration for a
// websocket-type API Gateway, locally.
//
// It implements http.Handler to handle websocket connections and invokes the provided AWS Lambda
// handler function for any CONNECT, DISCONNECT or MESSAGE events.
//
// It also implements the apigatewaymanagementapiiface.ApiGatewayManagementApiAPI client interface,
// so it can be used to write messages back to a client.
package awswebsocketadapter

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/apigatewaymanagementapi"
	"github.com/gorilla/websocket"
)

type LambdaHandler func(context.Context, events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error)

// Adapter is an implementation of an API Gateway Websocket API that invokes an AWS Lambda
// function in-memory. It is a handler that upgrades requests to websockets and invokes an AWS
// Lambda handler on each message. It also provides API Gateway Management APIs for writing back to
// connections.
type Adapter struct {
	LambdaHandler LambdaHandler

	upgrader websocket.Upgrader

	writersMu sync.Mutex
	writers   map[string]io.Writer
}

// ServeHTTP upgrades the request from HTTP to WS and then continues to send and receive websocket
// messages over the connection.
func (a *Adapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Upgrade the HTTP request to WS.
	ws, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer ws.Close()

	// Generate a random connection ID.
	var connIDSrc [8]byte
	if _, err := rand.Read(connIDSrc[:]); err != nil {
		log.Print("generate connection ID:", err)
		return
	}
	connID := base64.StdEncoding.EncodeToString(connIDSrc[:])

	// Invoke CONNECT handler.
	if err := a.invokeHandler(connID, "CONNECT", "", r.Header); err != nil {
		log.Println("handler:", err)
		return
	}

	defer func() {
		// Invoke DISCONNECT handler.
		if err := a.invokeHandler(connID, "DISCONNECT", "", r.Header); err != nil {
			log.Println("handler:", err)
		}
	}()

	// Register a hook for writing back to the connection, indexed by its connection ID.
	a.writersMu.Lock()
	if a.writers == nil {
		a.writers = make(map[string]io.Writer)
	}
	a.writers[connID] = &wsTextWriter{ws: ws}
	a.writersMu.Unlock()

	defer func() {
		a.writersMu.Lock()
		delete(a.writers, connID)
		a.writersMu.Unlock()
	}()

	// Read from the connection as long as it stays open.
	for {
		// Read the next message.
		mt, message, err := ws.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}

		// API Gateway Websockets only support text message types.
		if mt != websocket.TextMessage {
			log.Println("unsupported message type:", mt)
			break
		}

		// Invoke the Lambda handler
		if err := a.invokeHandler(connID, "MESSAGE", string(message), r.Header); err != nil {
			log.Println("handler:", err)
			if err := writeError(ws); err != nil {
				log.Println("write:", err)
				break
			}
		}
	}
}

func (a *Adapter) invokeHandler(connID, eventType, body string, header http.Header) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	res, err := a.LambdaHandler(ctx, events.APIGatewayWebsocketProxyRequest{
		RequestContext: events.APIGatewayWebsocketProxyRequestContext{
			ConnectionID: connID,
			EventType:    eventType,
		},
		MultiValueHeaders: header,
		Body:              body,
	})

	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status code: %d", res.StatusCode)
	}

	return nil
}

func writeError(ws *websocket.Conn) error {
	return ws.WriteMessage(websocket.TextMessage, []byte(`{"message": "Internal server error"}`))
}

func (a *Adapter) DeleteConnection(_ *apigatewaymanagementapi.DeleteConnectionInput) (*apigatewaymanagementapi.DeleteConnectionOutput, error) {
	panic("not implemented")
}

func (a *Adapter) DeleteConnectionWithContext(_ aws.Context, _ *apigatewaymanagementapi.DeleteConnectionInput, _ ...request.Option) (*apigatewaymanagementapi.DeleteConnectionOutput, error) {
	panic("not implemented")
}

func (a *Adapter) DeleteConnectionRequest(_ *apigatewaymanagementapi.DeleteConnectionInput) (*request.Request, *apigatewaymanagementapi.DeleteConnectionOutput) {
	panic("not implemented")
}

func (a *Adapter) GetConnection(_ *apigatewaymanagementapi.GetConnectionInput) (*apigatewaymanagementapi.GetConnectionOutput, error) {
	panic("not implemented")
}

func (a *Adapter) GetConnectionWithContext(_ aws.Context, _ *apigatewaymanagementapi.GetConnectionInput, _ ...request.Option) (*apigatewaymanagementapi.GetConnectionOutput, error) {
	panic("not implemented")
}

func (a *Adapter) GetConnectionRequest(_ *apigatewaymanagementapi.GetConnectionInput) (*request.Request, *apigatewaymanagementapi.GetConnectionOutput) {
	panic("not implemented")
}

func (a *Adapter) PostToConnection(input *apigatewaymanagementapi.PostToConnectionInput) (*apigatewaymanagementapi.PostToConnectionOutput, error) {
	return a.PostToConnectionWithContext(context.Background(), input)
}

func (a *Adapter) PostToConnectionWithContext(_ aws.Context, input *apigatewaymanagementapi.PostToConnectionInput, _ ...request.Option) (*apigatewaymanagementapi.PostToConnectionOutput, error) {
	var writer io.Writer

	a.writersMu.Lock()
	if a.writers != nil {
		writer = a.writers[*input.ConnectionId]
	}
	a.writersMu.Unlock()

	if writer == nil {
		return nil, &apigatewaymanagementapi.GoneException{}
	}

	_, err := writer.Write(input.Data)
	return &apigatewaymanagementapi.PostToConnectionOutput{}, err
}

func (a *Adapter) PostToConnectionRequest(_ *apigatewaymanagementapi.PostToConnectionInput) (*request.Request, *apigatewaymanagementapi.PostToConnectionOutput) {
	panic("not implemented")
}

type wsTextWriter struct {
	ws *websocket.Conn
}

func (w *wsTextWriter) Write(p []byte) (n int, err error) {
	return len(p), w.ws.WriteMessage(websocket.TextMessage, p)
}
