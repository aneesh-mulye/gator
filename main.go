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
	"strconv"
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
	commandRegistry.register("addfeed", middlewareLoggedIn(handlerAddfeed))
	commandRegistry.register("feeds", handlerFeeds)
	commandRegistry.register("follow", middlewareLoggedIn(handlerFollow))
	commandRegistry.register("following", middlewareLoggedIn(handlerFollowing))
	commandRegistry.register("unfollow", middlewareLoggedIn(handlerUnfollow))
	commandRegistry.register("browse", middlewareLoggedIn(handlerBrowse))
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
	if 1 != len(cmd.args) {
		return errors.New("'agg' requires one argument: time_between_reqs")
	}

	time_between_reqs, err := time.ParseDuration(cmd.args[0])
	if err != nil {
		return fmt.Errorf("Invalid duration '%s': %w", cmd.args[0], err)
	}

	ticker := time.NewTicker(time_between_reqs)
	for ; ; <-ticker.C {
		err = scrapeFeeds(s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scraping feed: %s\n", err.Error())
		}
	}
}

func handlerAddfeed(s *state, cmd command, user database.User) error {
	if 2 != len(cmd.args) {
		return errors.New("'addfeed' requires two arguments: addfeed <name> <url>")
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
			UserID:    user.ID,
		})

	if err != nil {
		return fmt.Errorf("Error adding feed for user %s: %w", user.Name, err)
	}

	fmt.Println(madeFeed)

	// Now, follow the feed
	err = handlerFollow(s,
		command{
			name: "follow",
			args: []string{feedURL},
		}, user)
	if err != nil {
		return fmt.Errorf("Error autofollowing newly created feed: %w", err)
	}

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

func handlerFollow(s *state, cmd command, user database.User) error {
	if 1 != len(cmd.args) {
		return errors.New("'follow' requires a feed URL argument")
	}
	// First, get the feed by URL.
	feedURL := cmd.args[0]
	feed, err := s.db.GetFeedByURL(context.Background(), feedURL)
	if err != nil {
		return fmt.Errorf("Error getting feed for URL '%s': %w", feedURL, err)
	}
	// Then, create the follow record.
	timeNow := time.Now()
	followRec, err := s.db.CreateFeedFollow(context.Background(),
		database.CreateFeedFollowParams{
			ID:        uuid.New(),
			CreatedAt: timeNow,
			UpdatedAt: timeNow,
			FeedID:    feed.ID,
			UserID:    user.ID,
		})
	if err != nil {
		return fmt.Errorf("Error following feed: %w", err)
	}
	// Then, print the name of the feed and current user.
	fmt.Printf("User '%s' is now following feed '%s'\n",
		followRec.UserName, followRec.FeedName)

	return nil
}

func handlerFollowing(s *state, cmd command, user database.User) error {
	if 0 != len(cmd.args) {
		return errors.New("'following' doesn't take any arguments")
	}

	feedsFollowing, err := s.db.GetFeedFollowsForUser(context.Background(),
		user.ID)
	if err != nil {
		return fmt.Errorf("Error getting feeds followed by user '%s': %w",
			user.Name, err)
	}

	fmt.Println("Feeds followed by " + user.Name + ":")
	for _, feed := range feedsFollowing {
		fmt.Println(feed.FeedName)
	}

	return nil
}

func handlerUnfollow(s *state, cmd command, user database.User) error {
	if 1 != len(cmd.args) {
		return errors.New("'unfollow' takes only the URL of the feed to unfollow")
	}
	// Get all the feeds for this user
	userFeeds, err := s.db.GetFeedFollowsForUser(context.Background(), user.ID)
	if err != nil {
		return fmt.Errorf("Error getting feeds for user '%s': %w", user.Name, err)
	}
	// Check if the user is in fact following a feed
	var userFollowsFeed bool
	var feedID uuid.UUID
	feedURL := cmd.args[0]
	for _, feed := range userFeeds {
		if feed.Url == feedURL {
			userFollowsFeed = true
			feedID = feed.FeedID
			break
		}
	}
	if !userFollowsFeed {
		return errors.New("you are not following this feed")
	}
	// If so, unfollow it
	err = s.db.UnfollowFeed(context.Background(),
		database.UnfollowFeedParams{
			UserID: user.ID,
			FeedID: feedID,
		})
	if err != nil {
		return fmt.Errorf("Error unfollowing feed: %w", err)
	}

	return nil
}

func handlerBrowse(s *state, cmd command, user database.User) error {
	if 0 != len(cmd.args) && 1 != len(cmd.args) {
		return fmt.Errorf("'browse' take at most one parameter: <limit>")
	}

	var postsToFetch int
	if 0 == len(cmd.args) {
		postsToFetch = 2
	} else {
		var err error
		postsToFetch, err = strconv.Atoi(cmd.args[0])
		if err != nil {
			return fmt.Errorf("Error parsing argument '%s' to number: %w",
				cmd.args[0], err)
		}
		if postsToFetch <= 0 {
			return fmt.Errorf("cannot fetch a non-positive number of posts")
		}
	}
	posts, err := s.db.GetPostsForUser(context.Background(),
		database.GetPostsForUserParams{
			ID:    user.ID,
			Limit: int32(postsToFetch),
		})
	if err != nil {
		return fmt.Errorf("Error getting user posts from database: %w", err)
	}

	for i, post := range posts {
		fmt.Println("Post " + strconv.Itoa(i+1))
		fmt.Println(post.Title)
		fmt.Println(post.Description)
		fmt.Println(post.Url)
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

func scrapeFeeds(s *state) error {
	feedRow, err := s.db.GetNextFeedToFetch(context.Background())
	if err != nil {
		return fmt.Errorf("Error getting feed '%s' from DB: %w", feedRow.Name, err)
	}

	err = s.db.MarkFeedFetched(context.Background(), feedRow.ID)
	if err != nil {
		return fmt.Errorf("Error marking feed '%s' fetched: %w", feedRow.Name, err)
	}

	feed, err := fetchFeed(context.Background(), feedRow.Url)
	if err != nil {
		return fmt.Errorf("Error fetching feed '%s': %w", feedRow.Name, err)
	}

	for _, item := range feed.Channel.Item {
		// Parse the time
		pubTime, err := time.Parse(time.RFC1123Z, item.PubDate)
		if err != nil {
			return fmt.Errorf("Couldn't parse date '%s' in feed '%s': %w",
				item.PubDate, feed.Channel.Title, err)
		}
		timeNow := time.Now()
		_, err = s.db.CreatePost(context.Background(),
			database.CreatePostParams{
				ID:          uuid.New(),
				CreatedAt:   timeNow,
				UpdatedAt:   timeNow,
				Title:       item.Title,
				Description: item.Description,
				PublishedAt: pubTime,
				FeedID:      feedRow.ID,
				Url:         item.Link,
			})
		if err != nil && err.Error() != "pq: duplicate key value violates unique constraint \"posts_url_key\"" {
			fmt.Println(err.Error())
		}
	}

	return nil
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

func middlewareLoggedIn(handler func(s *state, cmd command, user database.User) error) func(*state, command) error {
	return func(s *state, cmd command) error {
		loggedInUser := s.config.CurrentUserName
		userInfo, err := s.db.GetUser(context.Background(), loggedInUser)
		if err != nil {
			return fmt.Errorf("Error looking up currently logged in user %s: %w",
				loggedInUser, err)
		}

		return handler(s, cmd, userInfo)
	}
}
