package main

import (
    "encoding/base64"
    "encoding/json"
    "os"
    "time"

    "github.com/example/agent-native-microkernel/sdk/go/pluginhost"
    "github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func main(){
    dir:=os.Getenv("VIBE_DATA_DIR"); if err:=os.MkdirAll(dir,0o755);err!=nil{panic(err)}
    s,err:=Load(dir);if err!=nil{panic("session load: "+err.Error())}
    h:=pluginhost.New("org.vibe.session","1.0.0","")
    h.HandleContextCommand("session.seal",1,func(rc *pluginhost.RequestContext,e protocol.Envelope)(any,*protocol.Error){
        d:=sealDeps{Store:s,Replay:func()([]JournalRecord,error){
            all:=[]JournalRecord{}; after:=0
            for {
                resp,err:=rc.Query("event.journal.replay",1,map[string]int{"after":after,"limit":100},30*time.Second);if err!=nil{return nil,err}
                var page struct{Records []json.RawMessage `json:"records"`; Next int `json:"next"`}
                if err:=json.Unmarshal(resp.Payload,&page);err!=nil{return nil,err}
                for _,raw:=range page.Records{var r JournalRecord;if err:=json.Unmarshal(raw,&r);err!=nil{return nil,err};r.Raw=append(json.RawMessage(nil),raw...);all=append(all,r)}
                if page.Next<=after{return all,nil}; after=page.Next
            }
        },BlobPut:func(b []byte)(string,error){
            resp,err:=rc.Command("blob.put",1,map[string]string{"content_base64":base64.StdEncoding.EncodeToString(b)},30*time.Second);if err!=nil{return "",err}
            var out struct{URI string `json:"uri"`};if err:=json.Unmarshal(resp.Payload,&out);err!=nil{return "",err};return out.URI,nil
        }}
        return sealHandler(d)(rc,e)
    })
    h.HandleQuery("session.get",1,getHandler(s));h.HandleQuery("session.query",1,queryHandler(s))
    if err:=h.Serve();err!=nil{panic(err)}
}
