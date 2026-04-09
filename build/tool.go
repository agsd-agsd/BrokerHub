package build

import (
	"blockEmulator/params"
	"fmt"
	"log"
	"os"
	"path"
	"runtime"
)

var absolute_path = getABpath()

func getABpath() string {
	var abPath string
	_, filename, _, ok := runtime.Caller(1)
	if ok {
		abPath = path.Dir(filename)
	}
	return abPath
}

func GenerateBatFile(nodenum, shardnum, modID int, exchangeMode, feeOptimizerMode string, simSeed int64) {
	fileName := fmt.Sprintf("bat_shardNum=%v_NodeNum=%v_mod=%v.bat", shardnum, nodenum, params.CommitteeMethod[modID])
	ofile, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0777)
	if err != nil {
		log.Panic(err)
	}
	defer ofile.Close()
	ofile.WriteString("@echo off\n")
	ofile.WriteString("set APP=\n")
	ofile.WriteString("if exist \"%~dp0blockEmulator_Windows_Precompile.exe\" set APP=%~dp0blockEmulator_Windows_Precompile.exe\n")
	ofile.WriteString("if not defined APP if exist \"%~dp0blockEmulator.exe\" set APP=%~dp0blockEmulator.exe\n")
	ofile.WriteString("if not defined APP if exist \"%~dp0main.exe\" set APP=%~dp0main.exe\n")
	ofile.WriteString("if not defined APP (\n")
	ofile.WriteString("  echo Missing Windows executable. Run: go build -o blockEmulator_Windows_Precompile.exe .\n")
	ofile.WriteString("  pause\n")
	ofile.WriteString("  exit /b 1\n")
	ofile.WriteString(")\n\n")
	for i := 1; i < nodenum; i++ {
		for j := 0; j < shardnum; j++ {
			str := fmt.Sprintf("start \"\" cmd /k \"\"%%APP%%\" -n %d -N %d -s %d -S %d -m %d --exchange_mode %s --fee_optimizer %s --sim_seed %d\"\n\n", i, nodenum, j, shardnum, modID, exchangeMode, feeOptimizerMode, simSeed)
			ofile.WriteString(str)
		}
	}
	str := fmt.Sprintf("start \"\" cmd /k \"\"%%APP%%\" -c -N %d -S %d -m %d --exchange_mode %s --fee_optimizer %s --sim_seed %d\"\n\n", nodenum, shardnum, modID, exchangeMode, feeOptimizerMode, simSeed)

	ofile.WriteString(str)
	for j := 0; j < shardnum; j++ {
		str := fmt.Sprintf("start \"\" cmd /k \"\"%%APP%%\" -n 0 -N %d -s %d -S %d -m %d --exchange_mode %s --fee_optimizer %s --sim_seed %d\"\n\n", nodenum, j, shardnum, modID, exchangeMode, feeOptimizerMode, simSeed)
		ofile.WriteString(str)
	}
}

func GenerateShellFile(nodenum, shardnum, modID int, exchangeMode, feeOptimizerMode string, simSeed int64) {
	fileName := fmt.Sprintf("bat_shardNum=%v_NodeNum=%v_mod=%v.sh", shardnum, nodenum, params.CommitteeMethod[modID])
	ofile, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0777)
	if err != nil {
		log.Panic(err)
	}
	defer ofile.Close()
	str1 := fmt.Sprintf("#!/bin/bash \n\n")
	ofile.WriteString(str1)
	for j := 0; j < shardnum; j++ {
		for i := 1; i < nodenum; i++ {
			str := fmt.Sprintf("./blockEmulator_Linux_Precompile -n %d -N %d -s %d -S %d -m %d --exchange_mode %s --fee_optimizer %s --sim_seed %d &\n\n", i, nodenum, j, shardnum, modID, exchangeMode, feeOptimizerMode, simSeed)
			ofile.WriteString(str)
		}
	}
	str := fmt.Sprintf("./blockEmulator_Linux_Precompile -c -N %d -S %d -m %d --exchange_mode %s --fee_optimizer %s --sim_seed %d &\n\n", nodenum, shardnum, modID, exchangeMode, feeOptimizerMode, simSeed)

	ofile.WriteString(str)
	for j := 0; j < shardnum; j++ {
		str := fmt.Sprintf("./blockEmulator_Linux_Precompile -n 0 -N %d -s %d -S %d -m %d --exchange_mode %s --fee_optimizer %s --sim_seed %d &\n\n", nodenum, j, shardnum, modID, exchangeMode, feeOptimizerMode, simSeed)
		ofile.WriteString(str)
	}
}

func GenerateVBSFile() {
	ofile, err := os.OpenFile("batrun_HideWorker.vbs", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0777)
	if err != nil {
		log.Panic(err)
	}
	defer ofile.Close()
	nodenum := params.NodesInShard
	shardnum := params.ShardNum
	defaultModID := 4

	ofile.WriteString("Dim shell\n")
	ofile.WriteString("Dim fso\n")
	ofile.WriteString("Dim app\n")
	ofile.WriteString("Dim scriptDir\n")
	ofile.WriteString("Set shell = CreateObject(\"WScript.Shell\")\n")
	ofile.WriteString("Set fso = CreateObject(\"Scripting.FileSystemObject\")\n")
	ofile.WriteString("scriptDir = fso.GetParentFolderName(WScript.ScriptFullName)\n")
	ofile.WriteString("shell.CurrentDirectory = scriptDir\n")
	ofile.WriteString("app = \"\"\n")
	ofile.WriteString("If fso.FileExists(scriptDir & \"\\blockEmulator_Windows_Precompile.exe\") Then\n")
	ofile.WriteString("  app = scriptDir & \"\\blockEmulator_Windows_Precompile.exe\"\n")
	ofile.WriteString("ElseIf fso.FileExists(scriptDir & \"\\blockEmulator.exe\") Then\n")
	ofile.WriteString("  app = scriptDir & \"\\blockEmulator.exe\"\n")
	ofile.WriteString("ElseIf fso.FileExists(scriptDir & \"\\main.exe\") Then\n")
	ofile.WriteString("  app = scriptDir & \"\\main.exe\"\n")
	ofile.WriteString("Else\n")
	ofile.WriteString("  WScript.Echo \"Missing Windows executable. Run: go build -o blockEmulator_Windows_Precompile.exe .\"\n")
	ofile.WriteString("  WScript.Quit 1\n")
	ofile.WriteString("End If\n")

	for i := 1; i < nodenum; i++ {
		for j := 0; j < shardnum; j++ {
			str := fmt.Sprintf("shell.Run Chr(34) & app & Chr(34) & \" -n %d -N %d -s %d -S %d -m %d\", 0, false \n", i, nodenum, j, shardnum, defaultModID)
			ofile.WriteString(str)
		}
	}
	for j := 0; j < shardnum; j++ {
		str := fmt.Sprintf("shell.Run Chr(34) & app & Chr(34) & \" -n 0 -N %d -s %d -S %d -m %d\", 0, false \n", nodenum, j, shardnum, defaultModID)
		ofile.WriteString(str)
	}
	bfile, err := os.OpenFile("batrun_showSupervisor.bat", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0777)
	if err != nil {
		log.Panic(err)
	}
	defer bfile.Close()
	bfile.WriteString("@echo off\n")
	bfile.WriteString("set APP=\n")
	bfile.WriteString("if exist \"%~dp0blockEmulator_Windows_Precompile.exe\" set APP=%~dp0blockEmulator_Windows_Precompile.exe\n")
	bfile.WriteString("if not defined APP if exist \"%~dp0blockEmulator.exe\" set APP=%~dp0blockEmulator.exe\n")
	bfile.WriteString("if not defined APP if exist \"%~dp0main.exe\" set APP=%~dp0main.exe\n")
	bfile.WriteString("if not defined APP (\n")
	bfile.WriteString("  echo Missing Windows executable. Run: go build -o blockEmulator_Windows_Precompile.exe .\n")
	bfile.WriteString("  pause\n")
	bfile.WriteString("  exit /b 1\n")
	bfile.WriteString(")\n")
	bfile.WriteString("start \"\" batrun_HideWorker.vbs\n")
	str := fmt.Sprintf("start \"\" cmd /k \"\"%%APP%%\" -c -N %d -S %d -m %d\" \n", nodenum, shardnum, defaultModID)
	bfile.WriteString(str)
}
