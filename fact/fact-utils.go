package fact

import (
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"ChatWire/botlog"
	"ChatWire/cfg"
	"ChatWire/constants"
	"ChatWire/disc"
	"ChatWire/glob"
	"ChatWire/sclean"

	"github.com/bwmarrin/discordgo"
)

/* Delete old sav-*.zip, gen-*.zip files, to save space. */
func DeleteOldSav() {
	patha := cfg.Global.PathData.FactorioServersRoot + cfg.Global.PathData.FactorioHomePrefix + cfg.Local.ServerCallsign + "/" + cfg.Global.PathData.SaveFilePath + "/sav-*.zip"
	pathb := cfg.Global.PathData.FactorioServersRoot + cfg.Global.PathData.FactorioHomePrefix + cfg.Local.ServerCallsign + "/" + cfg.Global.PathData.SaveFilePath + "/gen-*.zip"

	var tempargs []string
	tempargs = append(tempargs, "-f")
	tempargs = append(tempargs, patha)
	tempargs = append(tempargs, pathb)

	out, errs := exec.Command(cfg.Global.PathData.RMPath, tempargs...).Output()

	if errs != nil {
		botlog.DoLog(fmt.Sprintf("Unable to delete old sav-*/gen-* map saves. Details:\nout: %v\nerr: %v", string(out), errs))
	} else {
		botlog.DoLog(fmt.Sprintf("Deleted old sav-*/gen-* map saves. Details:\nout: %v\nerr: %v", string(out), errs))
	}
}

/* Whitelist a specifc player. */
func WhitelistPlayer(pname string, level int) {
	if IsFactRunning() {
		if cfg.Local.DoWhitelist {
			if level > 0 {
				WriteFact(fmt.Sprintf("/whitelist add %s", pname))
			}
		}
	}
}

/* Write a full whitelist for a server, before it boots */
func WriteWhitelist() int {

	wpath := cfg.Global.PathData.FactorioServersRoot + cfg.Global.PathData.FactorioHomePrefix +
		cfg.Local.ServerCallsign + "/" + constants.WhitelistName

	if cfg.Local.DoWhitelist {
		glob.PlayerListLock.RLock()
		var count = 0
		var buf = "[\n"
		for _, player := range glob.PlayerList {
			if player.Level > 0 {
				buf = buf + "\"" + player.Name + "\",\n"
				count = count + 1
			}
		}
		lchar := len(buf)
		buf = buf[0 : lchar-2]
		buf = buf + "\n]\n"
		glob.PlayerListLock.RUnlock()

		_, err := os.Create(wpath)

		if err != nil {
			botlog.DoLog("WriteWhitelist: os.Create failure")
			return -1
		}

		err = ioutil.WriteFile(wpath, []byte(buf), 0644)

		if err != nil {
			botlog.DoLog("WriteWhitelist: WriteFile failure")
			return -1
		}
		return count
	} else {
		_ = os.Remove(wpath)
	}
	return 0
}

/* Quit factorio */
func QuitFactorio() {

	SetRelaunchThrottle(0)
	SetNoResponseCount(0)

	//Running but no players, just quit
	if IsFactorioBooted() && GetNumPlayers() <= 0 {
		WriteFact("/quit")

		//Running, but players connected... Give them quick feedback.
	} else if IsFactorioBooted() && GetNumPlayers() > 0 {
		WriteFact(fmt.Sprintf("/cchat %sServer quitting.[/color]", RandomColor(false)))
		WriteFact(fmt.Sprintf("/cchat %sServer quitting..[/color]", RandomColor(false)))
		WriteFact(fmt.Sprintf("/cchat %sServer quitting...[/color]", RandomColor(false)))
		time.Sleep(5 * time.Second)
		WriteFact("/quit")
	}
}

//Tell Factorio to save the map
func SaveFactorio() {

	if IsFactorioBooted() && 1 == 2 {
		gtime := GetGameTime()

		if gtime != constants.Unknown {
			WriteFact(fmt.Sprintf("/server-save sav-%s", gtime))
		} else {
			WriteFact("/server-save")
		}
	}
}

/* Send a string to Factorio, via stdin */
func WriteFact(input string) {
	PipeLock.Lock()
	defer PipeLock.Unlock()

	//Clean string
	buf := sclean.StripControlAndSubSpecial(input)

	gpipe := Pipe
	if gpipe != nil {

		plen := len(buf)

		if plen > 2000 {
			botlog.DoLog("Message to factorio, too long... Not sending.")
			return
		} else if plen <= 1 {
			botlog.DoLog("Message for factorio too short... Not sending.")
			return
		}

		_, err := io.WriteString(gpipe, buf+"\n")
		if err != nil {
			botlog.DoLog(fmt.Sprintf("An error occurred when attempting to write to Factorio. Details: %s", err))
			SetFactRunning(false, true)
			return
		}

	} else {
		botlog.DoLog("An error occurred when attempting to write to Factorio (nil pipe)")
		SetFactRunning(false, true)
		return
	}
}

/* Promote a player to the level they have, in Factorio and on Discord */
func AutoPromote(pname string) string {
	newusername := " *(New Player)* "

	if pname != "" {
		plevel := PlayerLevelGet(pname)
		if plevel == -1 {
			newusername = " *(Banned)*"

			WriteFact(fmt.Sprintf("/ban %s previously banned", pname))
		} else if plevel == 1 {
			newusername = " *(Member)*"

			WriteFact(fmt.Sprintf("/member %s", pname))
		} else if plevel == 2 {
			newusername = " *(Regular)*"

			WriteFact(fmt.Sprintf("/regular %s", pname))
		} else if plevel == 255 {
			newusername = " *(Admin)*"

			WriteFact(fmt.Sprintf("/promote %s", pname))
		}

		discid := disc.GetDiscordIDFromFactorioName(pname)
		factname := disc.GetFactorioNameFromDiscordID(discid)

		if factname == pname {

			newrole := ""
			if plevel == 1 {
				newrole = cfg.Global.RoleData.MemberRoleName
			} else if plevel == 2 {
				newrole = cfg.Global.RoleData.RegularRoleName
			} else if plevel == 255 {
				newrole = cfg.Global.RoleData.ModeratorRoleName
			} else {
				newrole = cfg.Global.RoleData.NewRoleName
			}

			guild := GetGuild()

			if guild != nil && disc.DS != nil {

				errrole, regrole := disc.RoleExists(guild, newrole)

				if errrole {
					errset := disc.SmartRoleAdd(cfg.Global.DiscordData.GuildID, discid, regrole.ID)
					if errset != nil {
						botlog.DoLog(fmt.Sprintf("Couldn't set role %v for %v.", plevel, pname))
					}
				}
			} else {

				botlog.DoLog("No guild data.")
			}
		}
	}

	return newusername

}

/* Update our channel name, but don't send it yet */
func UpdateChannelName() {

	var newchname string
	nump := GetNumPlayers()

	if nump == 0 {
		newchname = fmt.Sprintf("%v", cfg.Local.ServerCallsign+"-"+cfg.Local.Name)
	} else {
		newchname = fmt.Sprintf("%v🟢%v", nump, cfg.Local.ServerCallsign+"-"+cfg.Local.Name)
	}

	disc.UpdateChannelLock.Lock()
	disc.NewChanName = newchname
	disc.UpdateChannelLock.Unlock()

}

/* When appropriate, actually update the channel name */
func DoUpdateChannelName() {

	if disc.DS == nil {
		return
	}

	disc.UpdateChannelLock.Lock()
	chname := disc.NewChanName
	oldchname := disc.OldChanName
	disc.UpdateChannelLock.Unlock()

	if chname != oldchname && cfg.Local.ChannelData.ChatID != "" {
		disc.UpdateChannelLock.Lock()
		disc.OldChanName = disc.NewChanName
		disc.UpdateChannelLock.Unlock()

		_, aerr := disc.DS.ChannelEditComplex(cfg.Local.ChannelData.ChatID, &discordgo.ChannelEdit{Name: chname, Position: cfg.Local.ChannelData.Pos})

		if aerr != nil {
			botlog.DoLog(fmt.Sprintf("An error occurred when attempting to rename the Factorio discord channel. Details: %s", aerr))
		}
	}
}

/* Get a random color, used for Factorio text */
func RandomColor(justnumbers bool) string {
	var buf string

	if glob.LastColor < (constants.NumColors - 1) {
		glob.LastColor++
	} else {
		glob.LastColor = 0
	}

	color := constants.Colors[glob.LastColor]

	red := color.R + 0.2
	green := color.G + 0.2
	blue := color.B + 0.2

	if red > 1 {
		red = 1
	}
	if green > 1 {
		green = 1
	}
	if blue > 1 {
		blue = 1
	}

	if justnumbers {
		buf = fmt.Sprintf("%.2f,%.2f,%.2f", red, green, blue)
	} else {
		buf = fmt.Sprintf("[color=%.2f,%.2f,%.2f]", red, green, blue)
	}
	return buf
}

func ShowRewindList(s *discordgo.Session, m *discordgo.MessageCreate) {
	layoutUS := "01/02 03:04 PM MST"
	path := cfg.Global.PathData.FactorioServersRoot + cfg.Global.PathData.FactorioHomePrefix + cfg.Local.ServerCallsign + "/" + cfg.Global.PathData.SaveFilePath

	files, err := ioutil.ReadDir(path)
	//We can't read saves dir
	if err != nil {
		log.Fatal(err)
		CMS(m.ChannelID, "Something went wrong, sorry.")
	}

	rangeBuf := ""
	fileNames := ""
	lastNum := -1
	step := 1
	//Loop all files
	var tempf []fs.FileInfo
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "_autosave") && strings.HasSuffix(f.Name(), ".zip") {
			tempf = append(tempf, f)
		}
	}

	sort.Slice(tempf, func(i, j int) bool {
		return tempf[i].ModTime().After(tempf[j].ModTime())
	})

	maxList := constants.MaxRewindResults
	for _, f := range tempf {
		maxList--
		if maxList <= 0 {
			break
		}
		fName := f.Name()

		//Check if its a properly name autosave
		if strings.HasPrefix(fName, "_autosave") && strings.HasSuffix(fName, ".zip") {
			fTmp := strings.TrimPrefix(fName, "_autosave")
			fNumStr := strings.TrimSuffix(fTmp, ".zip")
			fNum, err := strconv.Atoi(fNumStr) //autosave number
			//Nope, no valid numer
			if err != nil {
				continue
			}
			step++

			//Get mod date
			modDate := f.ModTime().Local().Format(layoutUS)
			//Not first file add commas/newlines
			if fileNames != "" {
				if step%2 == 0 {
					fileNames = fileNames + "\n"
				} else {
					fileNames = fileNames + ",   "
				}
			}
			//Add to list with mod date
			fileNames = fileNames + fmt.Sprintf("(%15v ): autosave #%3v", modDate, fNum)

			//autosave number range list
			//If number is not sequential, save end of range and print it
			if fNum != lastNum-1 {
				//If we just started, add prefix, otherwise add dash and the end of the range, with comma for next item.
				if rangeBuf == "" {
					rangeBuf = "Autosaves:\n```"
				} else {
					rangeBuf = rangeBuf + "-" + strconv.Itoa(lastNum) + ", "
				}
				rangeBuf = rangeBuf + fmt.Sprintf("%v", fNum)
			}
			lastNum = fNum //Save for compairsion next loop
		}
	}
	//Add last item to range
	rangeBuf = rangeBuf + "-" + strconv.Itoa(lastNum)

	if lastNum == -1 {
		CMS(m.ChannelID, "No autosaves found.")
	} else {
		CMS(m.ChannelID, rangeBuf+"\n\n"+fileNames+"\n```")
	}
}

func DoRewindMap(s *discordgo.Session, m *discordgo.MessageCreate, arg string) {
	path := cfg.Global.PathData.FactorioServersRoot + cfg.Global.PathData.FactorioHomePrefix + cfg.Local.ServerCallsign + "/" + cfg.Global.PathData.SaveFilePath
	num, err := strconv.Atoi(arg)
	//Seems to be a number
	if err == nil {
		if num > 0 || num < 9999 {
			//Check if file is valid and found
			autoSaveStr := fmt.Sprintf("_autosave%v.zip", num)
			_, err := os.Stat(path + "/" + autoSaveStr)
			notfound := os.IsNotExist(err)

			if notfound {
				rewindSyntax(m)
				return
			} else {
				SetAutoStart(false)
				QuitFactorio()

				for x := 0; x < constants.MaxFactorioCloseWait && IsFactRunning(); x++ {
					time.Sleep(time.Second)
					if x == (constants.MaxFactorioCloseWait - 1) {
						CMS(m.ChannelID, "Factorio may be frozen, canceling rewind.")
						return
					}
				}
				selSaveName := path + "/" + autoSaveStr
				from, erra := os.Open(selSaveName)
				if erra != nil {
					botlog.DoLog(fmt.Sprintf("An error occurred when attempting to open the selected rewind map. Details: %s", erra))
				}
				defer from.Close()

				newmappath := path + "/" + cfg.Local.Name + "_new.zip"
				_, err := os.Stat(newmappath)
				if !os.IsNotExist(err) {
					err = os.Remove(newmappath)
					if err != nil {
						botlog.DoLog(fmt.Sprintf("An error occurred when attempting to remove the new map. Details: %s", err))
						return
					}
				}
				to, errb := os.OpenFile(newmappath, os.O_RDWR|os.O_CREATE, 0666)
				if errb != nil {
					botlog.DoLog(fmt.Sprintf("An error occurred when attempting to create the new rewind map. Details: %s", errb))
					return
				}
				defer to.Close()

				_, errc := io.Copy(to, from)
				if errc != nil {
					botlog.DoLog(fmt.Sprintf("An error occurred when attempting to write the new rewind map. Details: %s", errc))
					return
				}

				CMS(m.ChannelID, fmt.Sprintf("Loading autosave%v", num))
				time.Sleep(time.Second * 1)
				SetAutoStart(true)
				return
			}
		} else {
			rewindSyntax(m)
		}
	} else {
		rewindSyntax(m)
	}
}

func rewindSyntax(m *discordgo.MessageCreate) {
	CMS(m.ChannelID, "Not a valid autosave number, `"+cfg.Global.DiscordCommandPrefix+"rewind` to see a list of autosaves.")
	CMS(m.ChannelID, "Syntax: `"+cfg.Global.DiscordCommandPrefix+"rewind <autosave number>`")
}
