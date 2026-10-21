sim: sim.c ../gba/main.c ../gba/flash.c ../gba/save.c ../gba/status.c ../gba/*.h
	cc -O2 -g -Wall -DB2S_SIM -Dmain=gba_main -c ../gba/main.c -o main.o
	cc -O2 -g -Wall -DB2S_SIM -c ../gba/flash.c -o flash.o
	cc -O2 -g -Wall -DB2S_SIM -c ../gba/save.c -o save.o
	cc -O2 -g -Wall -DB2S_SIM -c ../gba/status.c -o status.o
	cc -O2 -g -Wall -DB2S_SIM sim.c main.o flash.o save.o status.o -o sim -lpthread
