package admin

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"ChatWire/cfg"
	"ChatWire/constants"
	"ChatWire/cwlog"
	"ChatWire/disc"
	"ChatWire/fact"
	"ChatWire/glob"
	"ChatWire/modupdate"
	"ChatWire/sclean"
)

func Factorio(s *discordgo.Session, i *discordgo.InteractionCreate) {

	//a := i.ApplicationCommandData()

}

/* RandomMap locks FactorioLaunchLock */
func NewMapPrev(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if fact.IsFactorioBooted() || fact.IsFactRunning() {
		buf := "Factorio is currently, running. You must stop the game first. See /stop-factorio"
		disc.EphemeralResponse(s, i, "Error:", buf)
		return
	}

	fact.FactorioLaunchLock.Lock()
	defer fact.FactorioLaunchLock.Unlock()

	//disc.EphemeralResponse(s, i, "Status:", "Generating map preview...")

	/* Make directory if it does not exist */
	newdir := fmt.Sprintf("%s/", cfg.Global.Paths.Folders.MapPreviews)
	err := os.MkdirAll(newdir, os.ModePerm)
	if err != nil {
		buf := fmt.Sprintf("Unable to create map preview directory: %v", err.Error())
		cwlog.DoLogCW(buf)
		elist := discordgo.MessageEmbed{Title: "Error:", Description: buf}
		disc.InteractionResponse(s, i, &elist)
		return
	}

	var preview_made = false
	t := time.Now()
	ourseed := int(t.UnixNano() - constants.CWEpoch)

	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.BigEndian, uint64(ourseed))
	fact.LastMapSeed = ourseed
	MapPreset := cfg.Local.Settings.MapPreset
	ourcode := fmt.Sprintf("%02d%v", fact.GetMapTypeNum(cfg.Local.Settings.MapPreset), base64.RawURLEncoding.EncodeToString(buf.Bytes()))
	fact.LastMapCode = ourcode

	path := fmt.Sprintf("%s%s.png", cfg.Global.Paths.Folders.MapPreviews, ourcode)
	jpgpath := fmt.Sprintf("%s%s.jpg", cfg.Global.Paths.Folders.MapPreviews, ourcode)
	args := []string{"--generate-map-preview", path, "--map-preview-size=" + cfg.Global.Options.PreviewSettings.PNGRes, "--map-preview-scale=" + cfg.Global.Options.PreviewSettings.PNGScale, "--map-gen-seed", fmt.Sprintf("%v", ourseed), cfg.Global.Options.PreviewSettings.Arguments}

	/* Append map gen if set */
	if cfg.Local.Settings.MapGenerator != "" && !strings.EqualFold(cfg.Local.Settings.MapGenerator, "none") {
		args = append(args, "--map-gen-settings")
		args = append(args, cfg.Global.Paths.Folders.ServersRoot+cfg.Global.Paths.Folders.MapGenerators+"/"+cfg.Local.Settings.MapGenerator+"-gen.json")

		args = append(args, "--map-settings")
		args = append(args, cfg.Global.Paths.Folders.ServersRoot+cfg.Global.Paths.Folders.MapGenerators+"/"+cfg.Local.Settings.MapGenerator+"-set.json")
	} else {
		args = append(args, "--preset")
		args = append(args, MapPreset)
	}

	lbuf := fmt.Sprintf("EXEC: %v ARGS: %v", fact.GetFactorioBinary(), strings.Join(args, " "))
	cwlog.DoLogCW(lbuf)
	cmd := exec.Command(fact.GetFactorioBinary(), args...)

	out, aerr := cmd.CombinedOutput()

	if aerr != nil {
		buf := fmt.Sprintf("An error occurred when attempting to generate the map preview: %s", aerr)
		cwlog.DoLogCW(buf)
		elist := discordgo.MessageEmbed{Title: "Error:", Description: buf}
		disc.InteractionResponse(s, i, &elist)
	}

	lines := strings.Split(string(out), "\n")

	for _, l := range lines {
		if strings.Contains(l, "Wrote map preview image file:") {
			preview_made = true
		}
	}
	if !preview_made {
		buf := "The game did not generate a map preview. Try clearing map-gen."
		cwlog.DoLogCW(buf)
		elist := discordgo.MessageEmbed{Title: "Error:", Description: buf}
		disc.InteractionResponse(s, i, &elist)
		return
	}

	imgargs := []string{path, "-quality", cfg.Global.Options.PreviewSettings.JPGQuality, "-scale", cfg.Global.Options.PreviewSettings.JPGScale, jpgpath}
	cmdb := exec.Command(cfg.Global.Paths.Binaries.ImgCmd, imgargs...)
	_, berr := cmdb.CombinedOutput()

	/* Delete PNG, we don't need it now */
	if err := os.Remove(path); err != nil {
		cwlog.DoLogCW("png preview file not found...")
	}

	if berr != nil {
		buf := fmt.Sprintf("An error occurred when attempting to convert the map preview: %s", berr)
		cwlog.DoLogCW(buf)
		elist := discordgo.MessageEmbed{Title: "Error:", Description: buf}
		disc.InteractionResponse(s, i, &elist)
		return
	}

	//Attempt to attach a map preview
	to, errb := os.OpenFile(jpgpath, os.O_RDONLY, 0666)
	if errb != nil {
		buf := fmt.Sprintf("Unable to read jpg file: %v", errb)
		cwlog.DoLogCW(buf)

		elist := discordgo.MessageEmbed{Title: "Error:", Description: buf}
		disc.InteractionResponse(s, i, &elist)
		return
	}
	defer to.Close()

	respData := &discordgo.InteractionResponseData{Files: []*discordgo.File{{Name: jpgpath, Reader: to}}}
	resp := &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: respData}
	err = s.InteractionRespond(i.Interaction, resp)
	if err != nil {
		cwlog.DoLogCW(err.Error())
	}
}

/* Generate map */
func MakeNewMap(s *discordgo.Session, i *discordgo.InteractionCreate) {

	if fact.IsFactorioBooted() || fact.IsFactRunning() {
		buf := "Factorio is currently, running. You must stop the game first. See /stop-factorio"
		disc.EphemeralResponse(s, i, "Error:", buf)
		return
	}

	fact.FactorioLaunchLock.Lock()
	defer fact.FactorioLaunchLock.Unlock()

	t := time.Now()
	ourseed := int(t.UnixNano() - constants.CWEpoch)
	MapPreset := cfg.Local.Settings.MapPreset

	if fact.LastMapSeed > 0 {
		ourseed = fact.LastMapSeed
	}

	//Use seed if specified, then clear it
	if cfg.Local.Settings.Seed > 0 {
		ourseed = cfg.Local.Settings.Seed
		cfg.WriteLCfg()
	}

	if ourseed <= 0 {
		buf := "Invalid seed data. (internal error)"
		cwlog.DoLogCW(buf)
		disc.EphemeralResponse(s, i, "Error:", buf)
		return
	}

	if MapPreset == "Error" {
		buf := "Invalid map preset."
		cwlog.DoLogCW(buf)
		disc.EphemeralResponse(s, i, "Error:", buf)
		return
	}

	disc.EphemeralResponse(s, i, "Status:", "Generating map...")

	/* Delete old sav-* map to save space */
	fact.DeleteOldSav()

	/* Generate code to make filename */
	buf := new(bytes.Buffer)

	_ = binary.Write(buf, binary.BigEndian, uint64(ourseed))
	ourcode := fmt.Sprintf("%02d%v", fact.GetMapTypeNum(MapPreset), base64.RawURLEncoding.EncodeToString(buf.Bytes()))
	filename := cfg.Global.Paths.Folders.ServersRoot + cfg.Global.Paths.FactorioPrefix + cfg.Local.Callsign + "/" + cfg.Global.Paths.Folders.Saves + "/gen-" + ourcode + ".zip"

	factargs := []string{"--map-gen-seed", fmt.Sprintf("%v", ourseed), "--create", filename}

	/* Append map gen if set */
	if cfg.Local.Settings.MapGenerator != "" && !strings.EqualFold(cfg.Local.Settings.MapGenerator, "none") {
		factargs = append(factargs, "--map-gen-settings")
		factargs = append(factargs, cfg.Global.Paths.Folders.ServersRoot+cfg.Global.Paths.Folders.MapGenerators+"/"+cfg.Local.Settings.MapGenerator+"-gen.json")

		factargs = append(factargs, "--map-settings")
		factargs = append(factargs, cfg.Global.Paths.Folders.ServersRoot+cfg.Global.Paths.Folders.MapGenerators+"/"+cfg.Local.Settings.MapGenerator+"-set.json")
	} else {
		factargs = append(factargs, "--preset")
		factargs = append(factargs, MapPreset)
	}

	lbuf := fmt.Sprintf("EXEC: %v ARGS: %v", fact.GetFactorioBinary(), strings.Join(factargs, " "))
	cwlog.DoLogCW(lbuf)

	cmd := exec.Command(fact.GetFactorioBinary(), factargs...)
	out, aerr := cmd.CombinedOutput()

	if aerr != nil {
		buf := fmt.Sprintf("An error occurred attempting to generate the map: %s", aerr)
		cwlog.DoLogCW(buf)
		var elist []*discordgo.MessageEmbed
		elist = append(elist, &discordgo.MessageEmbed{Title: "Error:", Description: buf})
		f := discordgo.WebhookParams{Embeds: elist}
		disc.FollowupResponse(s, i, &f)
		return
	}

	glob.VoteBoxLock.Lock()
	glob.VoteBox.LastRewindTime = time.Now()
	fact.VoidAllVotes()    /* Void all votes */
	fact.ResetTotalVotes() /* New map, reset player's vote limits */
	fact.WriteRewindVotes()
	glob.VoteBoxLock.Unlock()

	lines := strings.Split(string(out), "\n")

	for _, l := range lines {
		if strings.Contains(l, "Creating new map") {
			buf := fmt.Sprintf("New map saved as: %v", ourcode+".zip")
			var elist []*discordgo.MessageEmbed
			elist = append(elist, &discordgo.MessageEmbed{Title: "Complete:", Description: buf})
			f := discordgo.WebhookParams{Embeds: elist}
			disc.FollowupResponse(s, i, &f)
			return
		}
	}

	var elist []*discordgo.MessageEmbed
	elist = append(elist, &discordgo.MessageEmbed{Title: "Error:", Description: "Unknown error."})
	f := discordgo.WebhookParams{Embeds: elist}
	disc.FollowupResponse(s, i, &f)
}

/* Archive map */
func ArchiveMap(s *discordgo.Session, i *discordgo.InteractionCreate) {

	fact.GameMapLock.Lock()
	defer fact.GameMapLock.Unlock()

	version := strings.Split(fact.FactorioVersion, ".")
	vlen := len(version)

	if vlen < 3 {
		buf := "Unable to determine Factorio version."
		disc.EphemeralResponse(s, i, "Error:", buf)
	}

	if fact.GameMapPath != "" && fact.FactorioVersion != constants.Unknown {
		shortversion := strings.Join(version[0:2], ".")

		t := time.Now()
		date := t.Format("2006-01-02")

		newmapname := fmt.Sprintf("%v-%v-%v.zip", sclean.AlphaNumOnly(cfg.Local.Callsign), cfg.Local.Name, date)
		newmappath := fmt.Sprintf("%v%v%v/%v", cfg.Global.Paths.Folders.MapArchives, shortversion, constants.ArchiveFolderSuffix, newmapname)
		newmapurl := fmt.Sprintf("%v%v/%v", cfg.Global.Paths.URLs.ArchiveURL, url.PathEscape(shortversion+constants.ArchiveFolderSuffix), url.PathEscape(newmapname))

		from, erra := os.Open(fact.GameMapPath)
		if erra != nil {
			buf := fmt.Sprintf("An error occurred reading the map to archive: %s", erra)
			cwlog.DoLogCW(buf)
			disc.EphemeralResponse(s, i, "Error:", buf)
			return
		}
		defer from.Close()

		/* Make directory if it does not exist */
		newdir := fmt.Sprintf("%s%s%s/", cfg.Global.Paths.Folders.MapArchives, shortversion, constants.ArchiveFolderSuffix)
		err := os.MkdirAll(newdir, os.ModePerm)
		if err != nil {
			buf := fmt.Sprintf("Unable to create archive directory: %v", err.Error())
			cwlog.DoLogCW(buf)
			disc.EphemeralResponse(s, i, "Error:", buf)
			return
		}

		to, errb := os.OpenFile(newmappath, os.O_RDWR|os.O_CREATE, 0666)
		if errb != nil {
			buf := fmt.Sprintf("Unable to write archive file: %v", errb)
			cwlog.DoLogCW(buf)
			disc.EphemeralResponse(s, i, "Error:", buf)
			return
		}
		respData := &discordgo.InteractionResponseData{Content: newmapurl, Files: []*discordgo.File{
			{Name: newmapname, Reader: to, ContentType: "application/zip"}}}

		resp := &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: respData}
		err = s.InteractionRespond(i.Interaction, resp)
		if err != nil {
			cwlog.DoLogCW(err.Error())
		}
		defer to.Close()

		_, errc := io.Copy(to, from)
		if errc != nil {
			buf := fmt.Sprintf("Unable to write map archive file: %s", errc)
			cwlog.DoLogCW(buf)
			disc.EphemeralResponse(s, i, "Error:", buf)
			return
		}
		return

	} else {
		disc.EphemeralResponse(s, i, "Error:", "No map has been loaded yet.")
	}

}

/* Reboots Factorio only */
func StartFact(s *discordgo.Session, i *discordgo.InteractionCreate) {

	if fact.IsFactorioBooted() {

		buf := "Restarting Factorio..."
		disc.EphemeralResponse(s, i, "Status:", buf)
		fact.QuitFactorio("Server rebooting...")
	} else {
		buf := "Starting Factorio..."
		disc.EphemeralResponse(s, i, "Status:", buf)
	}

	fact.SetAutoStart(true)
	fact.SetRelaunchThrottle(0)
}

/*  StopServer saves the map and closes Factorio.  */
func StopFact(s *discordgo.Session, i *discordgo.InteractionCreate) {
	fact.SetRelaunchThrottle(0)
	fact.SetAutoStart(false)

	if fact.IsFactorioBooted() {

		buf := "Stopping Factorio."
		disc.EphemeralResponse(s, i, "Status:", buf)
		fact.QuitFactorio("Server quitting...")
	} else {
		buf := "Factorio isn't running, disabling auto-reboot."
		disc.EphemeralResponse(s, i, "Warning:", buf)
	}

}

/* Update Factorio  */
func UpdateFact(s *discordgo.Session, i *discordgo.InteractionCreate) {

	var args []string = strings.Split("", " ")
	argnum := len(args)

	if cfg.Global.Paths.Binaries.FactUpdater != "" {
		if argnum > 0 && strings.ToLower(args[0]) == "cancel" {
			fact.SetDoUpdateFactorio(false)
			cfg.Local.Options.AutoUpdate = false

			buf := "Update canceled, and auto-update disabled."
			disc.EphemeralResponse(s, i, "Status:", buf)
			return
		}
		fact.CheckFactUpdate(true)
	} else {
		buf := "The Factorio updater isn't configured."
		disc.EphemeralResponse(s, i, "Error:", buf)
	}
}

func UpdateMods(s *discordgo.Session, i *discordgo.InteractionCreate) {

	disc.EphemeralResponse(s, i, "Status:", "Checking for mod updates.")
	modupdate.CheckMods(true, true)
}