// handlers.project.go

package main

import (
	"net/http"
	"strconv"
	"fmt"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"


	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/expression"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

//from AWS CDK, set variables for DynamoDB Table and S3 bucket all the Project handling functions will use
var tableName = "UnicornDynamoDBVoting"
var bucket = "www.unicornpursuit.com"
var CurrentProjectNumber int

func loadProjectsDynamoDB(ddbsvc *dynamodb.DynamoDB) {

	proj := expression.NamesList(expression.Name("id"), expression.Name("title"), expression.Name("owner"), expression.Name("content"), expression.Name("photo"),expression.Name("votes"))
	expr, err := expression.NewBuilder().WithProjection(proj).Build()

		if err != nil {
    		fmt.Println("Error building expression:")
    		fmt.Println(err.Error())
    		os.Exit(1)
		}
		// Build the query input parameters
		params := &dynamodb.ScanInput{
			ExpressionAttributeNames:  expr.Names(),
			ExpressionAttributeValues: expr.Values(),
			FilterExpression:          expr.Filter(),
			ProjectionExpression:      expr.Projection(),
			TableName:                 aws.String(tableName),
		}

		// Make the DynamoDB Query API call
		result, err := ddbsvc.Scan(params)
		if err != nil {
			fmt.Println("Query API call failed:")
			fmt.Println((err.Error()))
			os.Exit(1)
		} else {
			// Unmarshall and sort the results	
			numItems := 1
			for _, i := range result.Items {
				item := ProjectExample{}

				err = dynamodbattribute.UnmarshalMap(i, &item)
				if err != nil {
					fmt.Println("Got error unmarshalling:")
					fmt.Println(err.Error())
					os.Exit(1)
				}
				if item.ID > 0 {
					numItems++
				}
				// Load Projects to memory, to avoid consulting DynamoDB for everything
				loadNewProject(item.ID, item.Title, item.Owner, item.Content, item.Photo, item.Votes)
			}
			CurrentProjectNumber = numItems
			fmt.Println(CurrentProjectNumber)
		}

}

func showIndexPage(c *gin.Context) {
	projects := getAllProjects()

	// Call the render function with the name of the template to render
	render(c, gin.H{
		"title":   "Home Page",
		"payload": projects}, "index.html")
}

func showProjectCreationPage(c *gin.Context) {
	// Call the render function with the name of the template to render
	render(c, gin.H{
		"title": "Create New Project"}, "create-project.html")
}

func getProject(c *gin.Context) {
	// Check if the project ID is valid
	if projectID, err := strconv.Atoi(c.Param("project_id")); err == nil {
		// Check if the project exists
		if project, err := getProjectByID(projectID); err == nil {
			// Call the render function with the title, project and the name of the
			// template
			render(c, gin.H{
				"title":   project.Title,
				"payload": project}, "project.html")

		} else {
			// If the project is not found, abort with an error
			c.AbortWithError(http.StatusNotFound, err)
		}

	} else {
		// If an invalid project ID is specified in the URL, abort with an error
		c.AbortWithStatus(http.StatusNotFound)
	}
}

func voteForProject(ddbsvc *dynamodb.DynamoDB) gin.HandlerFunc {
	fn := func(c *gin.Context) {
	votes := c.PostForm("votes")
	fmt.Println(votes)
	if projectID, err := strconv.Atoi(c.Param("project_id")); err == nil {
		// Check if the project exists
		if project, err := getProjectByID(projectID); err == nil {

			// Convert ID to String, required to pass it using UpdateItem function
			// We need ID and Owner as Primary Key to identify an Item we want to update
			ID := strconv.Itoa(project.ID)
			Owner := project.Owner

			// "r" is the Votes value user wants to be added to a project
			input := &dynamodb.UpdateItemInput{
				ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
					":r": {
						N: aws.String(votes),
					},
				},
				TableName: aws.String(tableName),
				Key: map[string]*dynamodb.AttributeValue{
					"id": {
						N: aws.String(ID),
					},
					"owner": {
						S: aws.String(Owner),
					},
					
				},
				ReturnValues:     aws.String("UPDATED_NEW"),
				// Using UpdateExpression, we will add the "votes" to the current value of the field (current number of votes)
				UpdateExpression: aws.String("set votes = votes + :r"),
			}

			_, err := ddbsvc.UpdateItem(input)
			if err != nil {
				c.HTML(http.StatusBadRequest, "project.html", gin.H{
					"ErrorTitle":   "Error updating Votes",
					"ErrorMessage": err.Error()})
			}

			// Dont forget to Scan DynamoDB into a local memory again
			
			projectList = nil
			loadProjectsDynamoDB(ddbsvc)

			// get project again, to be sure Vote value is updated
			project, err := getProjectByID(projectID)

			// Redirect to Voting SUccessful
			render(c, gin.H{
				"title":   "You've voted",
				"payload": project}, "voting-successful.html")

		} else {
			// If the project is not found, abort with an error
			c.AbortWithError(http.StatusNotFound, err)
		}

	} else {
		// If an invalid project ID is specified in the URL, abort with an error
		c.AbortWithStatus(http.StatusNotFound)
	}
	}
	return gin.HandlerFunc(fn)
}

func showLeaderboardPage(c *gin.Context) {

	// Get the sorted list of projects, starting with currently highest voted
		projects := getSortedProjects()

		// Call the render function with the name of the template to render
		render(c, gin.H{
			"title":   "Home Page",
			"payload": projects}, "leaderboard.html")
}

func createProject(ddbsvc *dynamodb.DynamoDB, sess *session.Session) gin.HandlerFunc {
	fn := func(c *gin.Context) {

		// Set the next project ID
		id := CurrentProjectNumber
		fmt.Println("Creating project with ID:")
		fmt.Println(id)

		// Obtain the POSTed project values
		title := c.PostForm("title")

		// Get owner as an email of the logged in user
		owner := c.PostForm("owner")
		content := c.PostForm("content")
		
		fileHeader, err := c.FormFile("file")
  		if err != nil {
			c.HTML(http.StatusBadRequest, "create-project.html", gin.H{
				"ErrorTitle":   "File Formation error",
				"ErrorMessage": err.Error()})
    	return
		} else {
			fmt.Println(fileHeader.Filename)
		}
		  
		f, err := fileHeader.Open()
		if err != nil {
			c.HTML(http.StatusBadRequest, "create-project.html", gin.H{
				"ErrorTitle":   "File Opening error",
				"ErrorMessage": err.Error()})
    	return
		}

		// Create an S3 Uploader
		uploader := s3manager.NewUploader(sess)

		// Upload 
		result, err := uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String(bucket),
			Key: aws.String(fileHeader.Filename),
			Body: f,
		})
		if err != nil {
			c.HTML(http.StatusBadRequest, "create-project.html", gin.H{
				"ErrorTitle":   "S3 Upload Failed",
				"ErrorMessage": err.Error()})
		} else {
			// Success, print URL to Console
			fmt.Println("Successfully uploaded to", result.Location)
		}

		// create a DynamoDB Item. Most information is retrieved from HTML "create-project.html"
		// Owner is the email address of the logged in user [to be implemented].
		// result.Location is a return URL value of S3 Photo Upload function.
		// Votes = 0, because we want all new projects to start with 0 votes.
		project := ProjectExample{
			ID: id,
			Title: title,
			Owner: owner,
			Content: content,
			Photo: result.Location,
			// Set Votes to 0, as it's a new project
			Votes: 0,
		}

		// Marshall new project into a map of AttributeValue objects.
		av, err := dynamodbattribute.MarshalMap(project)
		if err != nil {
			c.HTML(http.StatusBadRequest, "create-project.html", gin.H{
				"ErrorTitle":   "Error Marshalling a new projects",
				"ErrorMessage": err.Error()})
		}

		input := &dynamodb.PutItemInput{
			Item:      av,
			TableName: aws.String(tableName),
		}

  		// attempt PutItem with the created project object
		_, err = ddbsvc.PutItem(input)
		if err != nil {
			fmt.Println(err)
			c.HTML(http.StatusBadRequest, "create-project.html", gin.H{
				"ErrorTitle":   "Project Creation Failed",
				"ErrorMessage": err.Error()})
		} else {
			// Success, store project in DynamoDB and redirect to OK
			CurrentProjectNumber++
			loadNewProject(project.ID,project.Title,project.Owner,project.Content,project.Photo,project.Votes)
			render(c, gin.H{
				"title": "Project Created with Success"},
				"submission-successful.html")
		}

	}
	return gin.HandlerFunc(fn)
}