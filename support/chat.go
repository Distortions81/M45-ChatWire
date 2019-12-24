package support

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"time"

	"../glob/"
	"github.com/hpcloud/tail"
)

func PlayerFound(pname string) bool {
	glob.PlayerListLock.Lock()
	defer glob.PlayerListLock.Unlock()

	for i := 0; i < glob.PlayerListMax; i++ {
		if glob.PlayerList[i] == pname {

			//Found in list
			return true
		}
	}

	//Not in list, add them
	if glob.PlayerListMax < glob.MaxPlayers { //Don't go over max
		glob.PlayerList[glob.PlayerListMax] = pname
		glob.PlayerListMax++
	}

	writeplayers()
	return false
}

func writeplayers() {
		//Write to file
		glob.PlayerListWriteLock.Lock()
		defer glob.PlayerListWriteLock.Unlock()
		buffer := ""
	
		fo, err := os.Create(Config.DBFile)
		if err != nil {
			fmt.Println("Couldn't open db file.")
		}
		// close fo on exit and check for its returned error
		defer func() {
			if err := fo.Close(); err != nil {
				panic(err)
			}
		}()
	
		buffer = fmt.Sprintf("%d:", glob.NumLogins)
		for i := 0; i < glob.PlayerListMax; i++ {
			buffer = buffer + fmt.Sprintf("%s,", glob.PlayerList[i])
		}
	
		err = ioutil.WriteFile(Config.DBFile, []byte(buffer), 0644)
	
		if err != nil {
			fmt.Println("Couldn't write db file.")
		}
}

// Chat pipes in-game chat to Discord.
func Chat() {
	go func() {
		for {

			//Might not need to re-tail?
			t, err := tail.TailFile("factorio.log", tail.Config{Follow: true})
			if err != nil {
				ErrorLog(fmt.Errorf("%s: An error occurred when attempting to tail factorio.log\nDetails: %s", time.Now(), err))
			}
			for line := range t.Lines {
				time.Sleep(100 * time.Millisecond)
				//Ignore console messages
				if line.Text == "" {
					return
				}
				if len(line.Text) > 1900 {
					//Message too long
					return
				}

				if len(line.Text) > 0 && !strings.Contains(line.Text, "<server>") {
					if !strings.Contains(line.Text, "[CHAT]") {
						TmpList := strings.Split(line.Text, " ")

						//Send join/leave to Discord
						if strings.Contains(line.Text, "Online players") {

							poc := strings.Join(TmpList[2:], " ")
							poc = strings.ReplaceAll(poc, "(", "")
							poc = strings.ReplaceAll(poc, ")", "")
							poc = strings.ReplaceAll(poc, ":", "")

							oldch, errch := glob.DS.Channel(Config.FactorioChannelID)

							if errch == nil {
								newchname := ""
								oldchname := oldch.Name

								if poc == "0" {
									newchname = fmt.Sprintf("%s", Config.ChannelName)
								} else {
									newchname = fmt.Sprintf("%s %s online", Config.ChannelName, poc)
								}

								if newchname != oldchname {
									_, _ = glob.DS.ChannelEdit(Config.FactorioChannelID, newchname)
								}

								//_, _ = glob.DS.ChannelMessageSend(Config.FactorioChannelID, fmt.Sprintf("%s players online", poc))
							}
						}
						//Join message, with delay
						if strings.Contains(line.Text, "[JOIN]") {
							_, err = io.WriteString(glob.Pipe, "/p o c\r\n")

							if err != nil {
								ErrorLog(fmt.Errorf("%s: error when getting player count\nDetails: %s", time.Now(), err))
							}
							pname := TmpList[3]
							glob.NumLogins++
							newusername := ""
							if PlayerFound(pname) == false {
								newusername = "**(New Player)** "
								go func() {
									time.Sleep(20 * time.Second)
									_, err := io.WriteString(glob.Pipe, fmt.Sprintf("/w %s [color=0,1,1]Welcome! use tilde/tick ( ` or ~ key ) to chat![/color]\r\n", pname))
									time.Sleep(10 * time.Second)
									_, err = io.WriteString(glob.Pipe, fmt.Sprintf("/w %s [color=0,1,1]Check out our Discord server at: BHMM.NET![/color]\r\n", pname))
									time.Sleep(10 * time.Second)
									_, err = io.WriteString(glob.Pipe, fmt.Sprintf("/w %s [color=0,1,1]Please report griefers on the Discord, so we can ban them![/color]\r\n", pname))

									if err != nil {
										ErrorLog(fmt.Errorf("%s: error sending greeting\nDetails: %s", time.Now(), err))
									}
								}()
							}
							_, err := glob.DS.ChannelMessageSend(Config.FactorioChannelID, fmt.Sprintf("`%-13s` *%s %s(%d/%d)*", glob.Gametime, strings.Join(TmpList[3:], " "), newusername, glob.NumLogins, glob.PlayerListMax))
							if err != nil {
								ErrorLog(err)
							}

						}
						//Save on leave
						if strings.Contains(line.Text, "[LEAVE]") {
							_, err = io.WriteString(glob.Pipe, "/p o c\r\n")

							if err != nil {
								ErrorLog(fmt.Errorf("%s: error getting player count\nDetails: %s", time.Now(), err))
							}
							go func() {
								t := time.Now()
								// Don't save if we saved recently
								if t.Sub(glob.Sav_timer).Seconds() > 15 {

									_, err = io.WriteString(glob.Pipe, fmt.Sprintf("/server-save sav-%s\n", glob.Gametime))
									if err != nil {
										ErrorLog(fmt.Errorf("%s: Error when commanding LEAVE save.\nDetails: %s", time.Now(), err))
										glob.Running = false
									}
									glob.Sav_timer = time.Now()
								}

							}()
							_, err := glob.DS.ChannelMessageSend(Config.FactorioChannelID, fmt.Sprintf("`%-13s` *%s*", glob.Gametime, strings.Join(TmpList[3:], " ")))
							if err != nil {
								ErrorLog(err)
							}
						}
					} //End join/leave
					//Send chat to Discord
					if strings.Contains(line.Text, "[CHAT]") && !strings.Contains(line.Text, "<server>") {

						TmpList := strings.Split(line.Text, " ")
						TmpList[3] = strings.Replace(TmpList[3], ":", "", -1)

						cmess := strings.Join(TmpList[4:], " ")

						//Remove factorio tags
						rega := regexp.MustCompile(`\[[^][]+=[^][]+\]`) //remove [blah=blah]
						regb := regexp.MustCompile(`\[/[^][]+\]`)       //remove [/blah]
						regc := regexp.MustCompile(`\[gps=?.*\]`)       //replace [gps=x,y] with [Map Location]
						cmess = regc.ReplaceAllString(cmess, "[Map Location]")
						cmess = rega.ReplaceAllString(cmess, "${1}")
						cmess = regb.ReplaceAllString(cmess, "${1}")
						if len(cmess) > 300 {
							cmess = fmt.Sprintf("%300s**... (message cut, too long!)**", cmess)
						}

						_, err := glob.DS.ChannelMessageSend(Config.FactorioChannelID, fmt.Sprintf("`%-13s` **%s:** %s", glob.Gametime, TmpList[3], cmess))
						if err != nil {
							ErrorLog(err)
						}

					} //End Chat

					//Loading map
					if !strings.Contains(line.Text, "[CHAT]") && !strings.Contains(line.Text, "<server>") && strings.Contains(line.Text, "Loading map") {
						TmpList := strings.Split(line.Text, " ")

						_, err := glob.DS.ChannelMessageSend(Config.FactorioChannelID, fmt.Sprintf("%s", strings.Join(TmpList[4:7], " ")))
						if err != nil {
							ErrorLog(err)
						}
					}

					//Loading mod
					//                                        if !strings.Contains(line.Text,"[CHAT]") && !strings.Contains(line.Text,"<server>") && strings.Contains(line.Text,"Loading mod") &&
					//					!strings.Contains(line.Text,"settings") && !strings.Contains(line.Text,"base") && !strings.Contains(line.Text, "core") {
					//                                                TmpList := strings.Split(line.Text, " ")
					//
					//                                                glob.DS.ChannelMessageSend(Config.FactorioChannelID, fmt.Sprintf("(%s) %s", glob.Gametime, strings.Join(TmpList[4:8], " ")))
					//                                        }

					//Close message
					if !strings.Contains(line.Text, "[CHAT]") && !strings.Contains(line.Text, "<server>") && strings.Contains(line.Text, " Goodbye") {
						time.Sleep(2 * time.Second)
						_, err := glob.DS.ChannelMessageSend(Config.FactorioChannelID, "Factorio is now offline.")
						if err != nil {
							ErrorLog(err)
						}
						glob.Running = false
						if glob.Reboot == true {
							os.Exit(1)
						}
					}

					//Ready message
					if !strings.Contains(line.Text, "[CHAT]") && !strings.Contains(line.Text, "<server>") && strings.Contains(line.Text, " Matching server game ") && strings.Contains(line.Text, " has been created.") {
						_, err := glob.DS.ChannelMessageSend(Config.FactorioChannelID, "Factorio is now online!")
						if err != nil {
							ErrorLog(err)
						}
						_, err = io.WriteString(glob.Pipe, "/p o c\r\n")
						glob.Running = true
					}

					//Get in-game time
					ltl := strings.ToLower(line.Text)

					if (strings.Contains(ltl, " second") || strings.Contains(ltl, " minute") || strings.Contains(ltl, " hour") || strings.Contains(ltl, " day")) && !strings.Contains(line.Text, "[CHAT]") && !strings.Contains(line.Text, "<server>") {
						glob.Gametime = "gx-x-x-x"

						TmpList := strings.Split(ltl, " ")
						day := "0"
						hour := "0"
						minute := "0"
						second := "0"
						tmplen := len(TmpList)

						if tmplen > 1 {

							for x := 0; x < tmplen; x++ {
								if strings.Contains(TmpList[x], "day") {
									day = TmpList[x-1]
								} else if strings.Contains(TmpList[x], "hour") {
									hour = TmpList[x-1]
								} else if strings.Contains(TmpList[x], "minute") {
									minute = TmpList[x-1]
								} else if strings.Contains(TmpList[x], "second") {
									second = TmpList[x-1]
								}
							}
							glob.Gametime = fmt.Sprintf("g%s-%s-%s-%s", day, hour, minute, second)
						}

					}
					//Reset save timer
					if strings.Contains(line.Text, "Auto saving") || strings.Contains(line.Text, "Saving game") || strings.Contains(line.Text, "Saving Finished") || strings.Contains(line.Text, "[LEAVE]") {
						if !strings.Contains(line.Text, "[CHAT]") && !strings.Contains(line.Text, "<server>") {
							time.Sleep(time.Second)
							glob.Sav_timer = time.Now()
						}
					}
				} //End console filtered
			} //End Loop
		}
	}()
}
