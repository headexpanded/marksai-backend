package main

import (
    "context"
	"log"
	"os"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/core"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

func main() {
	app := pocketbase.New()

	anthropicApiKey := os.Getenv("ANTHROPIC_API_KEY")
    	if anthropicApiKey == "" {
    		log.Fatal("ANTHROPIC_API_KEY is not set in environment")
    	}

    anthropicClient := anthropic.NewClient()

	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {

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


	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
