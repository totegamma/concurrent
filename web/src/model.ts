
export interface Entity {
    ccaddr: string
    role: string
    host: string
    cdate: string
    score: number
}

export interface Host {
    fqdn: string
    ccaddr: string
    role: string
    score: number
    pubkey: string
    cdate: Date
}

export interface DomainProfile {
  nickname: string
  description: string
  logo: string
  wordmark: string
  themeColor: string
  rules: string
  tosURL: string
  maintainerName: string
  maintainerURL: string
  registration: string
  version: string
  hash: string
}

