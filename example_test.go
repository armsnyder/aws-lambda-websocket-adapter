package awswebsocketadapter_test

import (
	"context"
	"log"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go/service/apigatewaymanagementapi"
	"github.com/aws/aws-sdk-go/service/apigatewaymanagementapi/apigatewaymanagementapiiface"

	"github.com/armsnyder/awswebsocketadapter"
)

func Example() {
	// Create the adapter.
	var adapter awswebsocketadapter.Adapter

	// Set the LambdaHandler to a real lambda handler function.
	adapter.LambdaHandler = createHandler(&adapter)

	// Listen for ws:// requests.
	err := http.ListenAndServe(":8080", &adapter)
	log.Fatal(err)
}

// createHandler returns a handler function that can be passed to lambda.Start,
// from the aws-lambda-go SDK.
func createHandler(client apigatewaymanagementapiiface.ApiGatewayManagementApiAPI) func(context.Context, events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	return func(ctx context.Context, request events.APIGatewayWebsocketProxyRequest) (resp events.APIGatewayProxyResponse, err error) {
		resp = events.APIGatewayProxyResponse{StatusCode: 200}

		// As an example, send a message in response to every message received.
		// This demonstrates how Adapter can be used as an API Gateway Management API client.
		if request.RequestContext.EventType == "MESSAGE" {
			reply := &apigatewaymanagementapi.PostToConnectionInput{
				ConnectionId: &request.RequestContext.ConnectionID,
				Data:         []byte("hello"),
			}

			_, err = client.PostToConnectionWithContext(ctx, reply)
		}

		return resp, err
	}
}
