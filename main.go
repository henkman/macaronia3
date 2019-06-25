package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	twitch "github.com/gempir/go-twitch-irc"
	"github.com/henkman/steamquery"
)

type Alias struct {
	Command Command
	Format  string
}

type AliasUnresolved struct {
	Command string
	Format  string
}

type ServerInfo struct {
	Name       string
	Map        string
	Players    int
	MaxPlayers int
}

type Command = func(client *twitch.Client, user twitch.User, channel string, text string)

func getRegexFromCommandsAndAliases(cmdChar string, cmds map[string]Command, alias map[string]Alias) (*regexp.Regexp, error) {
	s := make([]string, 0, len(cmds)+len(alias))
	for c, _ := range cmds {
		s = append(s, c)
	}
	for a, _ := range alias {
		s = append(s, a)
	}
	return regexp.Compile("^" + cmdChar + "(" + strings.Join(s, "|") + `)\S?(.*?)$`)
}

func getServerInfo(address string) (ServerInfo, error) {
	const TRIES = 3
	for i := 0; ; i++ {
		var info ServerInfo
		rules, _, err := steamquery.QueryRulesString(address)
		if err != nil {
			if i == TRIES-1 {
				log.Println(err)
				return ServerInfo{}, err
			}
			time.Sleep(time.Millisecond * 250)
			continue
		}
		var numOpenPublicConnections int
		for _, r := range rules {
			if r.Name == "OwningPlayerName" {
				info.Name = r.Value
			} else if r.Name == "NumOpenPublicConnections" {
				tmp, err := strconv.Atoi(r.Value)
				if err != nil {
					return ServerInfo{}, err
				}
				numOpenPublicConnections = tmp
			} else if r.Name == "NumPublicConnections" {
				tmp, err := strconv.Atoi(r.Value)
				if err != nil {
					return ServerInfo{}, err
				}
				info.MaxPlayers = tmp
			} else if r.Name == "p2" {
				info.Map = r.Value
			}
		}
		info.Players = info.MaxPlayers - numOpenPublicConnections
		return info, nil
	}
}

func main() {
	{
		f, err := os.OpenFile("./log",
			os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0750)
		if err != nil {
			log.Panicln(err)
		}
		defer f.Close()
		log.SetOutput(f)
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}
	var config struct {
		Channels    []string `json:"channels"`
		Username    string   `json:"username"`
		Oauth       string   `json:"oauth"`
		CommandChar string   `json:"commandchar"`
	}
	{
		fd, err := os.Open("config.json")
		if err != nil {
			log.Panic(err)
		}
		err = json.NewDecoder(fd).Decode(&config)
		fd.Close()
		if err != nil {
			log.Panic(err)
		}
	}
	log.Printf("%+v\n", config)

	aliases := map[string]Alias{}

	var commands = map[string]Command{
		"help": func(client *twitch.Client, user twitch.User, channel string, text string) {
			client.Say(channel, "no")
		},
		"alias": func(client *twitch.Client, user twitch.User, channel string, text string) {
			// alias set name command text %s
			// alias rm name
			client.Say(channel, "NOT IMPLEMENTED")
		},
		"online": func() Command {
			reParams := regexp.MustCompile("^([^ ]+) ([^$]+)$")
			return func(client *twitch.Client, user twitch.User, channel string, text string) {
				// online server filter1;filterN
				m := reParams.FindStringSubmatch(text)
				if m == nil {
					return
				}
				filters := strings.Split(m[2], ";")
				players := []steamquery.Player{}
				const TRIES = 3
				for i := 0; ; i++ {
					pls, _, err := steamquery.QueryPlayersString(m[1])
					if err != nil {
						if i == TRIES-1 {
							log.Println(err)
							return
						}
						time.Sleep(time.Millisecond * 250)
						continue
					}
				nextPlayer:
					for _, player := range pls {
						for _, filter := range filters {
							if strings.Contains(player.Name, filter) {
								players = append(players, player)
								continue nextPlayer
							}
						}
					}
					break
				}
				if len(players) == 0 {
					return
				}
				var sb strings.Builder
				for _, p := range players {
					sb.WriteString(fmt.Sprintf("'%s'\t", p.Name))
				}
				client.Say(channel, sb.String())
			}
		}(),
		"info": func(client *twitch.Client, user twitch.User, channel string, text string) {
			// info serveraddress
			info, err := getServerInfo(text)
			if err != nil {
				log.Println(err)
				return
			}
			client.Say(channel, fmt.Sprintf("%d/%d on %s playing %s",
				info.Players, info.MaxPlayers, info.Name, info.Map))
		},
	}

	{ // alias restore
		fd, err := os.Open("aliases.json")
		if err != nil {
			log.Panic(err)
		}
		aus := map[string]AliasUnresolved{}
		err = json.NewDecoder(fd).Decode(&aus)
		fd.Close()
		if err != nil {
			log.Panic(err)
		}
		for k, au := range aus {
			cmd, ok := commands[au.Command]
			if !ok {
				log.Fatal("could not resolve alias", au.Command)
			}
			aliases[k] = Alias{
				Command: cmd,
				Format:  au.Format,
			}
		}
	}

	reCommands, err := getRegexFromCommandsAndAliases(config.CommandChar, commands, aliases)
	if err != nil {
		log.Panic(err)
	}

	client := twitch.NewClient(config.Username, config.Oauth)
	client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		m := reCommands.FindStringSubmatch(message.Message)
		if m == nil {
			return
		}
		if cmd, ok := commands[m[1]]; ok {
			fmt.Println("executing", m[2], "for user", message.User.Name)
			cmd(client, message.User, message.Channel, m[2])
		}
		if alias, ok := aliases[m[1]]; ok {
			var text string
			if strings.Contains(alias.Format, "%s") {
				text = fmt.Sprintf(alias.Format, m[2])
			} else {
				text = alias.Format
			}
			fmt.Println("executing", m[1], text, "for user", message.User.Name)
			alias.Command(client, message.User, message.Channel, text)
		}
	})
	for _, channel := range config.Channels {
		log.Println("joining channel", channel)
		client.Join(channel)
	}
	log.Println("connecting")
	if err := client.Connect(); err != nil {
		log.Panic(err)
	}
}
