param([string]$Work = "./.work")
Set-Location "$Work/egovframe-boot-sample-java-config"
mvn -B spring-boot:run
