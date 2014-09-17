package main

import (
	"fmt"
	"time"
	// "strings"
	
	"github.com/fiorix/go-eventsocket/eventsocket"
)


type Session struct {
	c                *eventsocket.Connection
	callerid         string
	callername       string
	inbound_uuid     string
	secretary_jobid  string
	secretary_uuid   string
}



func main() {
	fmt.Println("*** Started")
	eventsocket.ListenAndServe(":9090", outboundHandler)
	fmt.Println("*** Finished")
}

func outboundHandler(c *eventsocket.Connection) {

	fmt.Println("new client:", c.RemoteAddr())

	s := Session{}
	s.c = c
	
	{
		ev, err := c.Send("connect")
		if err != nil {
			fmt.Println("ERROR: ", err)
			return
		}

		s.callerid = ev.Get("Channel-Caller-Id-Number")
		s.callername = ev.Get("Channel-Caller-Id-Name")
		s.inbound_uuid = ev.Get("Channel-Unique-Id")
	}
	
	c.Send("linger")
	c.Execute("answer", "", true)

	go secretaryCallOut(&s)

	c.Execute("playback", "local_stream://moh", true)

	fmt.Println("playback aborted")

	normal_clearing := false
	{
		ev, err := c.Send("api uuid_getvar " + s.inbound_uuid +
			" hangup_cause")
		if err != nil && ev.Body != "" {
			normal_clearing = true
		}
	}
	
	hup := false

	if s.secretary_uuid == "" {
		hup = true
	} else {		
		ev, err := c.Send("api uuid_exists " + s.secretary_uuid)
		if err != nil || ev.Body == "false" {
			hup = true
		} else {
			// secretary is running
			if normal_clearing {
				// abort the secretary
				fmt.Println("Aborting the secretary")
				c.Send("api uuid_kill " + s.secretary_uuid)
			}
		}
	}

	if hup {
		c.Execute("playback", "misc/sorry.wav", true)
		c.Execute("sleep", "500", true)
		c.Execute("hangup", "", false)
		fmt.Println("hup")
	}
	
	c.Close()
}


func secretaryCallOut(s *Session) {

	c, err := eventsocket.Dial("localhost:8021", "ClueCon")
	if err != nil {
		fmt.Println("Failed to connect to FreeSWITCH: ", err)
		s.c.Execute("hangup", "", false)
		return
	}

	var uuid string
	
	{
		ev, err := c.Send("api create_uuid")
		if err != nil {
			fmt.Println("Failed uuid_create: ", err)
			s.c.Execute("hangup", "", false)
			return
		}

		uuid = ev.Body
	}

	s.secretary_uuid = uuid

	cmd := "api originate " + 
		fmt.Sprintf(
		"{ignore_early_media=true," +
		"origination_uuid=%s," +
		"originate_timeout=60,origination_caller_id_number=%s," +
		"origination_caller_id_name='%s'}user/%s",
		uuid,
		s.callerid,
		s.callername,
		"720@test01.voxserv.net") +
		" &playback(silence_stream://-1)"
	
	{
		_, err := c.Send(cmd)
		if err != nil {
			fmt.Println("Error calling out: ", err)
			c.Send("api uuid_break " + s.inbound_uuid)
			return
		}		
	}


	started := time.Now()
	digit := ""
	
	for time.Since(started).Seconds() < 60 && digit == "" {

		ev, err := c.Send("api uuid_exists " + s.secretary_uuid)
		if err != nil ||  ev.Body == "false" {
			break
		}
		
		digit = playAndGetOneDigit(
			"ivr/ivr-welcome_to_freeswitch.wav",
			c, s.secretary_uuid)
	}

	if digit == "" ||  digit == "0" {
		c.Send("api uuid_kill " + s.secretary_uuid)
		c.Send("api uuid_break " + s.inbound_uuid)
	} else {
		c.Send("api uuid_bridge " + s.inbound_uuid +
			" " + s.secretary_uuid)
	}	

	c.Close()
}




func playAndGetOneDigit(
	sound string,
	c *eventsocket.Connection,
	uuid string) string {
	
	c.Send("filter Unique-ID " + uuid)
        c.Send("event plain CHANNEL_EXECUTE_COMPLETE")

	{
		_, err := c.ExecuteUUID(
			uuid,
			"play_and_get_digits",
			"1 1 1 400 # " +
			sound + " silence_stream://250 result \\d")
		if err != nil {
			fmt.Println("Error executing play_and_get_digits: ",
				err)
			return ""
		}
	}

	finished := false
	ret := ""
	
	for !finished {

		ev, err := c.ReadEvent()
                if err != nil {
			finished = true
                } else {
			if ev.Get("Event-Name") ==
				"CHANNEL_EXECUTE_COMPLETE" &&
				ev.Get("Application") == "play_and_get_digits" {
				ret = ev.Get("Variable_result")
				finished = true
			}
		}
	}
	
	c.Send("noevents")
	c.Send("filter delete Unique-ID " + uuid)

	return ret
}


