import { Box, Button, Drawer, List, ListItem, ListItemButton, ListItemText, TextField, Typography } from "@mui/material"
import { forwardRef, useEffect, useState } from "react"
import { useApi } from "../context/apiContext"
import { Domain } from "@concurrent-world/client/dist/types/model/core"

export const Domains = forwardRef<HTMLDivElement>((props, ref): JSX.Element => {

    const { api } = useApi()

    const [domains, setDomains] = useState<Domain[]>([])
    const [remoteFqdn, setRemoteFqdn] = useState('')

    const [selectedDomain, setSelectedDomain] = useState<Domain | null>(null)
    const [newTag, setNewTag] = useState<string>('')
    const [newScore, setNewScore] = useState<number>(0)

    const refresh = () => {
        api.getDomains().then(setDomains)
    }

    useEffect(() => {
        refresh()
    }, [])

    return (
        <div ref={ref} {...props}>
            <Box
                width="100%"
            >
                <Box sx={{ display: 'flex', gap: '10px' }}>
                    <TextField
                        label="remote fqdn"
                        variant="outlined"
                        value={remoteFqdn}
                        sx={{ flexGrow: 1 }}
                        onChange={(e) => {
                            setRemoteFqdn(e.target.value)
                        }}
                    />
                    <Button
                        variant="contained"
                        onClick={(_) => {
                            api.addDomain(remoteFqdn)
                        }}
                    >
                        GO
                    </Button>
                </Box>
                <Typography>Domains</Typography>
                <List
                    disablePadding
                >
                    {domains.map((domain) => (
                        <ListItem key={domain.ccid}
                            disablePadding
                        >
                            <ListItemButton
                                onClick={() => {
                                    setNewTag(domain.tag)
                                    setNewScore(domain.score)
                                    setSelectedDomain(domain)
                                }}
                            >
                                <ListItemText primary={domain.fqdn} secondary={`${domain.ccid}`} />
                                <ListItemText>{`${domain.tag}(${domain.score})`}</ListItemText>
                            </ListItemButton>
                        </ListItem>
                    ))}
                </List>
            </Box>
            <Drawer
                anchor="right"
                open={selectedDomain !== null}
                onClose={() => {
                    setSelectedDomain(null)
                }}
            >
                <Box
                    width="50vw"
                    display="flex"
                    flexDirection="column"
                    gap={1}
                    padding={2}
                >
                    <Typography>{selectedDomain?.ccid}</Typography>
                    <pre>{JSON.stringify(selectedDomain, null, 2)}</pre>
                    <TextField
                        label="new tag"
                        variant="outlined"
                        value={newTag}
                        sx={{ flexGrow: 1 }}
                        onChange={(e) => {
                            setNewTag(e.target.value)
                        }}
                    />
                    <TextField
                        label="new score"
                        variant="outlined"
                        value={newScore}
                        sx={{ flexGrow: 1 }}
                        onChange={(e) => {
                            setNewScore(Number(e.target.value))
                        }}
                    />
                    <Button
                        variant="contained"
                        onClick={(_) => {
                            if (!selectedDomain) return
                            api.updateDomain({
                                ...selectedDomain,
                                score: newScore,
                                tag: newTag
                            }).then(() => {
                                refresh()
                                setSelectedDomain(null)
                            })
                        }}
                    >
                        Update
                    </Button>
                    <Button
                        variant="contained"
                        onClick={(_) => {
                            if (!selectedDomain) return
                            api.deleteDomain(selectedDomain.fqdn).then(() => {
                                refresh()
                                setSelectedDomain(null)
                            })
                        }}
                        color="error"
                    >
                        Delete
                    </Button>
                </Box>
            </Drawer>
        </div>
    )
})

Domains.displayName = "Domains"

