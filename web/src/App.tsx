import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { Register } from './pages/Register'
import { Box, Paper, Typography } from '@mui/material'
import { Home } from './pages/Home'
import { Login } from './pages/Login'
import { Welcome } from './pages/Welcome'
import { useEffect, useState } from 'react'
import { DomainProfile } from './model'

function App(): JSX.Element {

    const [profile, setProfile] = useState<DomainProfile | null>(null)

    useEffect(() => {
        console.log('fetching profile')
        fetch('/api/v1/domain', {
            method: 'GET',
        }).then(response => {
            if (response.ok) {
                return response.json()
            } else {
                throw new Error('Something went wrong')
            }
        }).then(data => {
            setProfile(data.content.meta)
            console.log(data)
        }).catch(error => {
            console.log(error)
        })
    }, [])


    return (
        <Box
            width='100vw'
            height='100dvh'
            display='flex'
            flexDirection='column'
            alignItems='center'
            padding='30px'
            gap='20px'
        >
            <Box
                display='flex'
                flexDirection='row'
                width='100%'
                maxWidth='900px'
                alignItems='center'
                justifyContent='space-between'
            >
                <Box
                    display='flex'
                    flexDirection='column'
                >
                    <Typography
                        fontSize='20px'
                        fontWeight='bold'
                    >
                        Concrnt Domain
                    </Typography>
                    <Typography
                        fontSize='30px'
                        fontWeight='bold'
                    >
                        {window.location.hostname}
                    </Typography>
                </Box>
                <Box
                    display='flex'
                    flexDirection='row'
                    alignItems='center'
                    gap='10px'
                >
                    Powered By
                    <Box
                        component="img"
                        height="50px"
                        borderRadius={1}
                        src={profile?.wordmark}
                    />
                </Box>
            </Box>
            <Paper
                variant='outlined'
                sx={{
                    width: '100%',
                    maxWidth: '900px',
                    padding: '20px',
                    flex: 1
                }}
            >
                <BrowserRouter basename="/web">
                    <Routes>
                        <Route path="/" element={<Home />} />
                        <Route path="/login" element={<Login />} />
                        <Route path="/welcome" element={<Welcome />} />
                        <Route path="/register" element={<Register profile={profile}/>} />
                    </Routes>
                </BrowserRouter>
            </Paper>
        </Box>
    )
}

export default App
