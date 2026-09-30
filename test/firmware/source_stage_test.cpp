// Compile the actual sketch against a deterministic Arduino I/O harness.
// This checks firmware behavior, not AVR compilation or physical motor wiring.
#include <cassert>
#include <cstdint>
#include <deque>
#include <string>
#include <iostream>

using byte=uint8_t;
constexpr int HIGH=1, LOW=0, OUTPUT=1;
uint64_t elapsedUS=0;
int pulses=0, injectStopAt=0;
bool injectKeepalives=false;
uint64_t nextKeepaliveUS=0;
struct SerialHarness {
 std::deque<char> input;
 std::string output;
 void begin(int){}
 int available(){return input.size();}
 char read(){char c=input.front();input.pop_front();return c;}
 void print(const char *s){output+=s;}
 void print(char c){output+=c;}
 void print(long n){output+=std::to_string(n);}
 void println(const char *s){print(s);output+='\n';}
 void println(long n){print(n);output+='\n';}
} Serial;
void pinMode(int,int){}
void digitalWrite(int pin,int level){
 if((pin==2||pin==5)&&level==HIGH){
  ++pulses;
  if(injectStopAt && pulses==injectStopAt)Serial.input.push_back('S');
 }
}
unsigned long millis(){return elapsedUS/1000;}
void delayMicroseconds(unsigned int us){
 elapsedUS+=us;
 if(injectKeepalives && elapsedUS>=nextKeepaliveUS){
  Serial.input.push_back('K');Serial.input.push_back('\n');
  nextKeepaliveUS=elapsedUS+250000;
 }
}

#include "../../firmware/pet_xy_stage/pet_xy_stage.ino"

std::string send(const std::string &s){
 Serial.output.clear();for(char c:s+"\n")Serial.input.push_back(c);
 loop();return Serial.output;
}

int main(){
 setup();assert(pulses==0);
 assert(send("HELLO")=="PETXY 1\n");
 assert(send("MOVE 1 2")=="ERROR command\n");
 assert(send("CONFIG 20 3000 1 1")=="CONFIGURED\n");
 assert(send("ZERO")=="ZEROED\n");
 assert(send("MOVE 3 -2")=="READY 3 -2\n");
 assert(pulses==5&&xSteps==3&&ySteps==-2);
 assert(send("MOVE 3 -2")=="READY 3 -2\n");assert(pulses==5);
 assert(send("MOVE nonsense 2")=="ERROR position\n");
 assert(send("MOVE 10000001 0")=="ERROR position\n");
 injectStopAt=pulses+2;
 assert(send("MOVE 100 0").find("ERROR stop_requested x=5 y=-2 heartbeat_age_ms=")==0);
 assert(xSteps==5&&ySteps==-2&&!zeroed);
 assert(send("MOVE 0 0")=="ERROR command\n");
 injectStopAt=0;
 assert(send("ZERO")=="ZEROED\n");
 assert(send("MOVE 1000 0").find("ERROR heartbeat_timeout x=")==0);
 assert(xSteps>0&&xSteps<1000&&ySteps==0&&!zeroed);
 assert(send("S")=="STOPPED\n");
 assert(send("ZERO")=="ZEROED\n");
 injectKeepalives=true;
 assert(send("MOVE -10200 10200")=="READY -10200 10200\n");
 assert(xSteps==-10200&&ySteps==10200&&zeroed);
 injectKeepalives=false;
 assert(send(std::string(100,'X'))=="ERROR line_too_long\n");
 std::cout<<"source stage firmware checks passed\n";
}
