Write-Host "== eGovFrame VSCode 핸즈온 사전 점검 =="
function Need($name) {
  if (Get-Command $name -ErrorAction SilentlyContinue) {
    Write-Host "  [O] $name"
  } else {
    Write-Host "  [X] $name (미설치)"
  }
}
Need git; Need mvn; Need node; Need npm; Need go; Need docker
java -version
