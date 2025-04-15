package main

import (
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/aneesh-mulye/gator/internal/config"
	"github.com/aneesh-mulye/gator/internal/database"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type state struct {
	db     *database.Queries
	config *config.Config
}

type command struct {
	name string
	args []string
}

type commands struct {
	handlers map[string]func(*state, command) error
}

func (c *commands) register(name string, f func(*state, command) error) error {
	c.handlers[name] = f
	return nil
}

func (c *commands) run(s *state, cmd command) error {
	if nil == c.handlers[cmd.name] {
		return fmt.Errorf("No such command: %s", cmd.name)
	}

	err := c.handlers[cmd.name](s, cmd)
	if err != nil {
		return err
	}

	return nil
}

var commandRegistry commands

func init() {
	commandRegistry.handlers = make(map[string]func(*state, command) error)
	commandRegistry.register("login", handlerLogin)
	commandRegistry.register("register", handlerRegister)
	commandRegistry.register("reset", handlerReset)
	commandRegistry.register("users", handlerUsers)
	commandRegistry.register("agg", handlerAgg)
	commandRegistry.register("addfeed", handlerAddfeed)
	commandRegistry.register("feeds", handlerFeeds)
}

func main() {
	var appState state
	c, err := config.Read()
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	appState.config = &c

	db, err := sql.Open("postgres", appState.config.DbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to database: %s", err.Error())
	}

	dbQueries := database.New(db)
	appState.db = dbQueries

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "No command specified\n")
		os.Exit(1)
	}

	err = commandRegistry.run(&appState,
		command{name: os.Args[1], args: os.Args[2:]})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
}

func handlerLogin(s *state, cmd command) error {
	if 0 == len(cmd.args) {
		return fmt.Errorf("No username specified")
	}

	if 1 < len(cmd.args) {
		return fmt.Errorf("Only one username allowed")
	}

	userToLogin := cmd.args[0]
	_, err := s.db.GetUser(context.Background(), userToLogin)
	if err != nil {
		return fmt.Errorf("Could not login user %s: %w", userToLogin, err)
	}

	err = s.config.SetUser(userToLogin)
	if err != nil {
		return fmt.Errorf("Error logging in: %w", err)
	}

	fmt.Println("user set to: '" + userToLogin + "'")

	return nil
}

func handlerRegister(s *state, cmd command) error {
	if 0 == len(cmd.args) {
		return fmt.Errorf("No username specified")
	}

	if 1 < len(cmd.args) {
		return fmt.Errorf("Only one username allowed")
	}

	userToCreate := cmd.args[0]
	timeNow := time.Now()
	userRet, err := s.db.CreateUser(context.Background(),
		database.CreateUserParams{
			ID:        uuid.New(),
			CreatedAt: timeNow,
			UpdatedAt: timeNow,
			Name:      userToCreate,
		})
	if err != nil {
		return fmt.Errorf("Could not create user: %w", err)
	}

	err = s.config.SetUser(userToCreate)
	if err != nil {
		return fmt.Errorf("Error logging in with newly created user: %w", err)
	}

	fmt.Println("user created and logged in")
	fmt.Println(userRet)

	return nil
}

func handlerReset(s *state, cmd command) error {
	if 0 != len(cmd.args) {
		return errors.New("'reset' takes no arguments")
	}

	err := s.db.Reset(context.Background())
	if err != nil {
		return fmt.Errorf("Could not reset users: %w", err)
	}

	return nil
}

func handlerUsers(s *state, cmd command) error {
	if 0 != len(cmd.args) {
		return errors.New("'users' takes no arguments")
	}

	users, err := s.db.GetUsers(context.Background())
	if err != nil {
		return fmt.Errorf("Error fetching users: %w", err)
	}

	for _, user := range users {
		fmt.Print(string(user))
		if user == s.config.CurrentUserName {
			fmt.Print(" (current)")
		}
		fmt.Println()
	}

	return nil
}

func handlerAgg(s *state, cmd command) error {
	if 0 != len(cmd.args) {
		return errors.New("'agg' takes no arguments")
	}

	feed, err := fetchFeed(context.Background(),
		"https://www.wagslane.dev/index.xml")
	if err != nil {
		return err
	}

	fmt.Println(feed)
	return nil
}

func handlerAddfeed(s *state, cmd command) error {
	if 2 != len(cmd.args) {
		return errors.New("'addfeed' requires two arguments: addfeed <name> <url>")
	}

	// Look up current user's ID.
	userForWhomFeed := s.config.CurrentUserName
	userInfo, err := s.db.GetUser(context.Background(), userForWhomFeed)
	if err != nil {
		return fmt.Errorf("Error adding feed for user %s: %w", userForWhomFeed, err)
	}

	feedName := cmd.args[0]
	feedURL := cmd.args[1]
	timeNow := time.Now()
	madeFeed, err := s.db.CreateFeed(context.Background(),
		database.CreateFeedParams{
			ID:        uuid.New(),
			CreatedAt: timeNow,
			UpdatedAt: timeNow,
			Name:      feedName,
			Url:       feedURL,
			UserID:    userInfo.ID,
		})

	if err != nil {
		return fmt.Errorf("Error adding feed for user %s: %w", userForWhomFeed, err)
	}

	fmt.Println(madeFeed)

	return nil
}

func handlerFeeds(s *state, cmd command) error {
	if 0 != len(cmd.args) {
		return errors.New("'feeds' takes no arguments")
	}

	feeds, err := s.db.GetFeeds(context.Background())
	if err != nil {
		return fmt.Errorf("Error getting feeds: %w", err)
	}

	for i, feed := range feeds {
		fmt.Printf("%d) Feed: %s\n", (i + 1), feed.Name)
		fmt.Printf(" - URL: %s\n", feed.Url)
		fmt.Printf(" - User: %s\n", feed.Username)
		fmt.Println()
	}

	return nil
}

func fetchFeed(ctx context.Context, feedURL string) (*RSSFeed, error) {
	// First, create and fill in the request.
	req, err := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gator")
	// Then, perform it.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// Then, read into a data buffer.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// Then, unmarshal from the data buffer into the struct
	var feed RSSFeed
	err = xml.Unmarshal(body, &feed)
	if err != nil {
		return nil, err
	}
	// Then unescapte it.
	unescapeFeed(&feed)
	// Then (*shiver*) return a pointer to it. (!!!???!!!)
	return &feed, nil
}

func unescapeFeed(feed *RSSFeed) {
	feed.Channel.Title = html.UnescapeString(feed.Channel.Title)
	feed.Channel.Description = html.UnescapeString(feed.Channel.Description)

	for i := range len(feed.Channel.Item) {
		feed.Channel.Item[i].Title = html.UnescapeString(feed.Channel.Item[i].Title)
		feed.Channel.Item[i].Description =
			html.UnescapeString(feed.Channel.Item[i].Description)
	}
}

type RSSFeed struct {
	Channel struct {
		Title       string    `xml:"title"`
		Link        string    `xml:"link"`
		Description string    `xml:"description"`
		Item        []RSSItem `xml:"item"`
	} `xml:"channel"`
}

type RSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}
