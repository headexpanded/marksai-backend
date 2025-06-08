package main

import (
    "context"
	"log"
	"os"
	"sync"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/core"
	"github.com/gorilla/websocket"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		// Allow all origins for development
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	// Map to store active WebSocket connections
	clients = make(map[*websocket.Conn]bool)
	// Mutex to protect the clients map
	clientsMutex sync.Mutex
)

func main() {
	app := pocketbase.New()

	anthropicApiKey := os.Getenv("ANTHROPIC_API_KEY")
    	if anthropicApiKey == "" {
    		log.Fatal("ANTHROPIC_API_KEY is not set in environment")
    	}

    anthropicClient := anthropic.NewClient()

	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {
		// Add the authentication middleware
		e.Router.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				// Get the auth record from the request
				authRecord, _ := c.Get("authRecord").(*models.Record)
				if authRecord != nil {
					c.Set("pb_user", authRecord)
				}
				return next(c)
			}
		})

		// WebSocket endpoint
		e.Router.GET("/ws", func(c echo.Context) error {
			ws, err := upgrader.Upgrade(c.Response().Writer, c.Request(), nil)
			if err != nil {
				log.Printf("WebSocket upgrade error: %v", err)
				return err
			}
			defer ws.Close()

			// Add client to the map
			clientsMutex.Lock()
			clients[ws] = true
			clientsMutex.Unlock()

			// Remove client when function returns
			defer func() {
				clientsMutex.Lock()
				delete(clients, ws)
				clientsMutex.Unlock()
			}()

			// Handle incoming messages
			for {
				_, message, err := ws.ReadMessage()
				if err != nil {
					log.Printf("WebSocket read error: %v", err)
					break
				}

				// Process the message with Claude
				ctx := context.Background()
				messages := []anthropic.MessageParam{
					anthropic.NewUserMessage(anthropic.NewTextBlock(string(message))),
				}

				response, err := anthropicClient.Messages.New(ctx, anthropic.MessageNewParams{
					Model:     anthropic.ModelClaude3_7SonnetLatest,
					MaxTokens: int64(1024),
					Messages:  messages,
				})
				if err != nil {
					log.Printf("Anthropic error: %v", err)
					ws.WriteMessage(websocket.TextMessage, []byte("Error processing request"))
					continue
				}

				// Aggregate AI response text
				var aiResponse string
				for _, content := range response.Content {
					if content.Type == "text" {
						aiResponse += content.Text
					}
				}

				// Send response back to client
				if err := ws.WriteMessage(websocket.TextMessage, []byte(aiResponse)); err != nil {
					log.Printf("WebSocket write error: %v", err)
					break
				}

				// Save conversation to PocketBase
				collection, err := app.Dao().FindCollectionByNameOrId("conversations")
				if err != nil {
					log.Printf("Failed to find conversations collection: %v", err)
					continue
				}

				record := models.NewRecord(collection)
				recordData := map[string]any{
					"userInput":  string(message),
					"aiResponse": aiResponse,
				}
				record.Load(recordData)

				if err := app.Dao().SaveRecord(record); err != nil {
					log.Printf("Failed to save conversation: %v", err)
				}
			}

			return nil
		})

    		// Protect the route: must be authenticated user
    		e.Router.POST("/api/ask/ai", func(c echo.Context) error {

    			// Get the current authenticated user from context
    			user := c.Get("pb_user")
    			if user == nil {
    				return c.JSON(401, map[string]string{"error": "unauthorized"})
    			}

    			// Type assertion for the user
    			pbUser, ok := user.(*models.Record)
    			if !ok {
    				log.Printf("Failed to assert user type from context: %T", user)
    				return c.JSON(500, map[string]string{"error": "failed to retrieve user information"})
    			}

    			type requestBody struct {
    				Input string `json:"input"`
    			}
    			var body requestBody
    			if err := c.Bind(&body); err != nil {
    				return c.JSON(400, map[string]string{"error": "invalid request"})
    			}

    			// Call Claude AI
    			ctx := context.Background()

    			// Prepare message
    			messages := []anthropic.MessageParam{
    				anthropic.NewUserMessage(anthropic.NewTextBlock(body.Input)),
    			}

    			response, err := anthropicClient.Messages.New(ctx, anthropic.MessageNewParams{
    				Model:     anthropic.ModelClaude3_7SonnetLatest,
    				MaxTokens: int64(1024),
    				Messages:  messages,
    			})
    			if err != nil {
    				log.Printf("Anthropic error: %v", err)
    				return c.JSON(500, map[string]string{"error": "AI service error"})
    			}

    			// Aggregate AI response text
    			var aiResponse string
    			for _, content := range response.Content {
    				if content.Type == "text" {
    					aiResponse += content.Text
    				}
    			}

    			// Save conversation to PocketBase
    			collection, err := app.Dao().FindCollectionByNameOrId("conversations")
    			if err != nil {
    				log.Printf("Failed to find conversations collection: %v", err)
    				return c.JSON(500, map[string]string{"error": "conversations collection not found"})
    			}

    			// Create a new record instance for the "conversations" collection
    			record := models.NewRecord(collection)

    			recordData := map[string]any{
    				"user":       pbUser.Id, // Use pbUser.Id after type assertion
    				"userInput":  body.Input,
    				"aiResponse": aiResponse,
    			}

    			// Load the data into the record
    			record.Load(recordData)

    			// Save the record
    			if err := app.Dao().SaveRecord(record); err != nil { // SaveRecord returns only an error
    				log.Printf("Failed to save conversation: %v", err)
    				return c.JSON(500, map[string]string{"error": "failed to save conversation"})
    			}

    			return c.JSON(200, map[string]string{"response": aiResponse})
    		})

    		return nil
    	})

    	if err := app.Start(); err != nil {
    		log.Fatal(err)
    	}
}
