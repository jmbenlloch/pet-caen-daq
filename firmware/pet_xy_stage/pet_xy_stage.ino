/* PETXY/1 host-controlled replacement for the JANUS autonomous raster sketch.
 * Same TB6600 wiring: X PUL=D2 DIR=D3, Y PUL=D5 DIR=D6, common GND.
 * No movement at boot. 115200 baud, ASCII lines, max 95 bytes.
 * Coordinates are signed absolute steps from an operator-confirmed origin.
 * There are no limit switches/encoders: configure a mechanically valid grid.
 */
#include <stdlib.h>
#include <string.h>

const int X_PUL=2, X_DIR=3, Y_PUL=5, Y_DIR=6;
long xSteps=0, ySteps=0;
unsigned int highUS=20, lowUS=3000;
bool xPositive=true, yPositive=true, configured=false, zeroed=false;
char line[96]; byte used=0;
bool overflow=false, stopped=false;
unsigned long heartbeat=0;
const char *motionError="unknown";
unsigned long heartbeatAge=0;

// All pulse bookkeeping follows the emitted pulse, including an interrupted
// move. A watchdog/stop invalidates the origin until a new explicit ZERO.
bool moveAxis(int pul,int dir,long target,long &position,bool positive) {
 digitalWrite(dir,(target>position)==positive?HIGH:LOW);
 delayMicroseconds(30);
 while(position!=target) {
  while(Serial.available()) {
   char c=Serial.read();
   if(c=='S') stopped=true;
   if(c=='K') heartbeat=millis();
  }
  heartbeatAge=millis()-heartbeat;
  if(stopped || heartbeatAge>1000) {
   motionError=stopped?"stop_requested":"heartbeat_timeout";
   zeroed=false;return false;
  }
  digitalWrite(pul,HIGH);delayMicroseconds(highUS);
  digitalWrite(pul,LOW);delayMicroseconds(lowUS);
  position+=(target>position?1:-1);
 }
 return true;
}

bool number(char *token,long &value) {
 if(!token||!*token)return false;
 char *end;value=strtol(token,&end,10);
 return *end==0;
}

void command() {
 char *name=strtok(line," \r");if(!name)return;
 if(!strcmp(name,"K"))return;
 if(!strcmp(name,"HELLO")){Serial.println("PETXY 1");return;}
 if(!strcmp(name,"S")){zeroed=false;Serial.println("STOPPED");return;}
 if(!strcmp(name,"CONFIG")) {
  long hi,lo,x,y;
  bool ok=number(strtok(NULL," \r"),hi)&&number(strtok(NULL," \r"),lo)&&number(strtok(NULL," \r"),x)&&number(strtok(NULL," \r"),y);
  if(!ok||strtok(NULL," \r")||hi<2||hi>10000||lo<2||lo>10000||x<0||x>1||y<0||y>1){Serial.println("ERROR config");return;}
  highUS=hi;lowUS=lo;xPositive=x;yPositive=y;configured=true;zeroed=false;
  Serial.println("CONFIGURED");return;
 }
 if(!strcmp(name,"ZERO")&&configured){xSteps=0;ySteps=0;zeroed=true;Serial.println("ZEROED");return;}
 if(!strcmp(name,"MOVE")&&zeroed) {
  long x,y;
  bool ok=number(strtok(NULL," \r"),x)&&number(strtok(NULL," \r"),y);
  if(!ok||strtok(NULL," \r")||x< -10000000L||x>10000000L||y< -10000000L||y>10000000L){Serial.println("ERROR position");return;}
  stopped=false;heartbeat=millis();
  if(!moveAxis(X_PUL,X_DIR,x,xSteps,xPositive)||!moveAxis(Y_PUL,Y_DIR,y,ySteps,yPositive)){
   Serial.print("ERROR ");Serial.print(motionError);
   Serial.print(" x=");Serial.print(xSteps);Serial.print(" y=");Serial.print(ySteps);
   Serial.print(" heartbeat_age_ms=");Serial.println((long)heartbeatAge);return;
  }
  Serial.print("READY ");Serial.print(xSteps);Serial.print(' ');Serial.println(ySteps);return;
 }
 Serial.println("ERROR command");
}

void setup(){
 pinMode(X_PUL,OUTPUT);pinMode(X_DIR,OUTPUT);pinMode(Y_PUL,OUTPUT);pinMode(Y_DIR,OUTPUT);
 digitalWrite(X_PUL,LOW);digitalWrite(Y_PUL,LOW);Serial.begin(115200);
}
void loop(){
 while(Serial.available()){
  char c=Serial.read();
  if(c=='\n'){
   if(overflow)Serial.println("ERROR line_too_long");else{line[used]=0;command();}
   used=0;overflow=false;
  } else if(used<sizeof(line)-1){line[used++]=c;}else{overflow=true;}
 }
}
